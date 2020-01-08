package lifecycle

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alexkappa/mustache"

	"hub/config"
	"hub/manifest"
	"hub/parameters"
	"hub/util"
)

const (
	templateSuffix   = ".template"
	curlyKind        = "curly"
	mustacheKind     = "mustache"
	trueMustacheKind = "_mustache"
	goKind           = "go"
)

var kinds = []string{curlyKind, mustacheKind, trueMustacheKind, goKind}

type TemplateRef struct {
	Filename string
	Kind     string
}

type OpenErr struct {
	Filename string
	Error    error
}

func processTemplates(component *manifest.ComponentRef, templateSetup *manifest.TemplateSetup,
	params parameters.LockedParameters, outputs parameters.CapturedOutputs,
	dir string) []error {

	componentName := manifest.ComponentQualifiedNameFromRef(component)
	kv := parameters.ParametersKV(params)
	templateSetup, err := expandParametersInTemplateSetup(templateSetup, kv)
	if err != nil {
		return []error{err}
	}
	templates := scanTemplates(componentName, dir, templateSetup)

	if config.Verbose {
		if len(templates) > 0 {
			log.Print("Component templates:")
			printTemplates(templates)
		} else if len(templateSetup.Files) > 0 || len(templateSetup.Directories) > 0 || len(templateSetup.Extra) > 0 {
			log.Printf("No templates for component `%s`", componentName)
		}
	}

	if len(templates) == 0 {
		return nil
	}

	filenames := make([]string, 0, len(templates))
	hasMustache := false
	for _, template := range templates {
		filenames = append(filenames, template.Filename)
		if !hasMustache && template.Kind == mustacheKind {
			hasMustache = true
		}
	}
	cannot := checkStat(filenames)
	if len(cannot) > 0 {
		diag := make([]string, 0, len(cannot))
		for _, e := range cannot {
			diag = append(diag, fmt.Sprintf("\t`%s`: %v", e.Filename, e.Error))
		}
		return []error{fmt.Errorf("Unable to open `%s` component template input(s):\n%s", componentName, strings.Join(diag, "\n"))}
	}

	// during lifecycle operation `outputs` is nil - only parameters are available in templates
	// outputs are for `hub render`
	if outputs != nil {
		kv = parameters.ParametersAndOutputsKV(params, outputs)
	}
	if hasMustache {
		kv = addMustacheCompatibleBindings(kv)
	}
	errs := make([]error, 0)
	for _, template := range templates {
		errs = append(errs, processTemplate(template, componentName, component.Depends, kv)...)
	}
	return errs
}

func maybeExpandParametersInTemplateGlob(glob string, kv map[string]string, section string, index int) (string, error) {
	if !parameters.RequireExpansion(glob) {
		return glob, nil
	}
	value, errs := expandParametersInTemplateGlob(fmt.Sprintf("%s.%d", section, index), glob, kv)
	if len(errs) > 0 {
		return "", fmt.Errorf("Failed to expand template globs:\n\t%s", util.Errors("\n\t", errs...))
	}
	return value, nil
}

func expandParametersInTemplateGlob(what string, value string, kv map[string]string) (string, []error) {
	piggy := manifest.Parameter{Name: fmt.Sprintf("templates.%s", what), Value: value}
	errs := parameters.ExpandParameter(&piggy, []string{}, kv)
	return piggy.Value, errs
}

func expandParametersInTemplateSetup(templateSetup *manifest.TemplateSetup,
	kv map[string]string) (*manifest.TemplateSetup, error) {

	setup := manifest.TemplateSetup{
		Kind:        templateSetup.Kind,
		Files:       make([]string, 0, len(templateSetup.Files)),
		Directories: make([]string, 0, len(templateSetup.Directories)),
		Extra:       make([]manifest.TemplateTarget, 0, len(templateSetup.Extra)),
	}

	for i, glob := range templateSetup.Files {
		expanded, err := maybeExpandParametersInTemplateGlob(glob, kv, "files", i)
		if err != nil {
			return nil, err
		}
		setup.Files = append(setup.Files, expanded)
	}
	for i, glob := range templateSetup.Directories {
		expanded, err := maybeExpandParametersInTemplateGlob(glob, kv, "directories", i)
		if err != nil {
			return nil, err
		}
		setup.Directories = append(setup.Directories, expanded)
	}
	for j, templateExtra := range templateSetup.Extra {
		extra := manifest.TemplateTarget{
			Kind:        templateExtra.Kind,
			Files:       make([]string, 0, len(templateExtra.Files)),
			Directories: make([]string, 0, len(templateExtra.Directories)),
		}

		prefix := fmt.Sprintf("extra.%d", j)

		prefix2 := fmt.Sprintf("%s.files", prefix)
		for i, glob := range templateExtra.Files {
			expanded, err := maybeExpandParametersInTemplateGlob(glob, kv, prefix2, i)
			if err != nil {
				return nil, err
			}
			extra.Files = append(extra.Files, expanded)
		}
		prefix2 = fmt.Sprintf("%s.directories", prefix)
		for i, glob := range templateExtra.Directories {
			expanded, err := maybeExpandParametersInTemplateGlob(glob, kv, prefix2, i)
			if err != nil {
				return nil, err
			}
			extra.Directories = append(extra.Directories, expanded)
		}

		setup.Extra = append(setup.Extra, extra)
	}

	return &setup, nil
}

func scanTemplates(componentName string, baseDir string, templateSetup *manifest.TemplateSetup) []TemplateRef {
	templates := make([]TemplateRef, 0, 10)

	templateSetup.Kind = checkKind(componentName, templateSetup.Kind)
	templates = appendPlainFiles(templates, baseDir, templateSetup.Files, templateSetup.Kind)
	templates = scanDirectories(componentName, templates, baseDir, templateSetup.Directories, templateSetup.Files, templateSetup.Kind)

	for _, extra := range templateSetup.Extra {
		extra.Kind = checkKind(componentName, extra.Kind)
		templates = appendPlainFiles(templates, baseDir, extra.Files, extra.Kind)
		templates = scanDirectories(componentName, templates, baseDir, extra.Directories, extra.Files, extra.Kind)
	}
	return templates
}

func appendPlainFiles(acc []TemplateRef, baseDir string, files []string, kind string) []TemplateRef {
	for _, file := range files {
		if !isGlob(file) {
			filePath := path.Join(baseDir, file)
			if !strings.HasSuffix(file, templateSuffix) {
				info, err := os.Stat(filePath + templateSuffix)
				if err == nil && !info.IsDir() {
					filePath = filePath + templateSuffix
				}
			}
			acc = append(acc, TemplateRef{Filename: filePath, Kind: kind})
		}
	}
	return acc
}

func scanDirectories(componentName string, acc []TemplateRef, baseDir string, directories []string, files []string, kind string) []TemplateRef {
	if len(files) == 0 && len(directories) > 0 {
		files = []string{"*"}
	}
	if len(directories) == 0 {
		directories = []string{""}
	}
	for _, dir := range directories {
		for _, file := range files {
			if isGlob(file) {
				glob := path.Join(baseDir, dir, file)
				if config.Debug {
					log.Printf("Scanning for `%s` templates `%s`", componentName, glob)
				}
				matches, err := filepath.Glob(glob)
				if err != nil {
					util.Warn("Unable to expand `%s` component template glob `%s`: %v", componentName, glob, err)
				}
				if matches != nil {
					for _, file := range matches {
						acc = append(acc, TemplateRef{Filename: file, Kind: kind})
					}
				} else {
					util.Warn("No matches found for `%s` component template glob `%s`", componentName, glob)
				}
			}
		}
	}
	return acc
}

func isGlob(path string) bool {
	return strings.Contains(path, "*") || strings.Contains(path, "[")
}

func checkKind(componentName string, kind string) string {
	if kind == "" {
		return curlyKind
	}
	if !util.Contains(kinds, kind) {
		util.Warn("Component `%s` template kind `%s` not recognized; supported %v",
			componentName, kind, kinds)
	}
	return kind
}

func checkStat(templates []string) []OpenErr {
	cannot := make([]OpenErr, 0)
	for _, template := range templates {
		info, err := os.Stat(template)
		if err != nil {
			cannot = append(cannot, OpenErr{Filename: template, Error: err})
		} else if info.IsDir() {
			cannot = append(cannot, OpenErr{Filename: template, Error: errors.New("is a directory")})
		}
	}
	if len(cannot) == 0 {
		return nil
	}
	return cannot
}

func processTemplate(template TemplateRef, componentName string, componentDepends []string,
	kv map[string]string) []error {

	tmpl, err := os.Open(template.Filename)
	if err != nil {
		return []error{fmt.Errorf("Unable to open `%s` component template input `%s`: %v", componentName, template.Filename, err)}
	}
	byteContent, err := ioutil.ReadAll(tmpl)
	if err != nil {
		return []error{fmt.Errorf("Unable to read `%s` component template content `%s`: %v", componentName, template.Filename, err)}
	}
	statInfo, err := tmpl.Stat()
	if err != nil {
		util.Warn("Unable to stat `%s` component template input `%s`: %v",
			componentName, template.Filename, err)
	}
	tmpl.Close()
	content := string(byteContent)

	outPath := template.Filename
	if strings.HasSuffix(outPath, templateSuffix) {
		outPath = outPath[:len(outPath)-len(templateSuffix)]
	}
	out, err := os.Create(outPath)
	if err != nil {
		return []error{fmt.Errorf("Unable to open `%s` component template output `%s`: %v", componentName, outPath, err)}
	}
	defer out.Close()
	if statInfo != nil {
		err = out.Chmod(statInfo.Mode())
		if err != nil {
			util.Warn("Unable to chmod `%s` component template output `%s`: %v",
				componentName, template.Filename, err)
		}
	}

	var outContent string
	var errs []error
	switch template.Kind {
	case "", curlyKind:
		outContent, errs = processReplacement(content, template.Filename, componentName, componentDepends,
			kv, curlyReplacement, stripCurly)
	case mustacheKind:
		outContent, errs = processReplacement(content, template.Filename, componentName, componentDepends, kv,
			mustacheReplacement, stripMustache)
	case trueMustacheKind:
		outContent, err = processMustache(content, template.Filename, componentName, componentDepends, kv)
		if err != nil {
			errs = append(errs, err)
		}
	default:
		errs = append(errs, fmt.Errorf("Error processing `%s` component template `%s`: unknown `%s` template kind",
			componentName, outPath, template.Kind))
		return errs
	}

	if len(outContent) > 0 {
		written, err := strings.NewReader(outContent).WriteTo(out)
		if err != nil || written != int64(len(outContent)) {
			errs = append(errs, fmt.Errorf("Error writting `%s` component template output `%s`: %v", componentName, outPath, err))
		}
	}

	return errs
}

var (
	curlyReplacement    = regexp.MustCompile("\\$\\{[a-zA-Z0-9_\\.\\|:/-]+\\}")
	mustacheReplacement = regexp.MustCompile("\\{\\{[a-zA-Z0-9_\\.\\|:/-]+\\}\\}")
)

func stripCurly(match string) string {
	return match[2 : len(match)-1]
}

func stripMustache(match string) string {
	return match[2 : len(match)-2]
}

func valueEncoding(variable string) (string, string) {
	i := strings.Index(variable, "/")
	if i <= 0 || i == len(variable)-1 {
		return variable, ""
	}
	return variable[0:i], variable[i+1:]
}

func processReplacement(content, filename, componentName string, componentDepends []string,
	kv map[string]string, replacement *regexp.Regexp, strip func(string) string) (string, []error) {

	errs := make([]error, 0)
	replaced := false

	outContent := replacement.ReplaceAllStringFunc(content,
		func(variable string) string {
			variable = strip(variable)
			variable, encoding := valueEncoding(variable)
			substitution, exist := parameters.FindValue(variable, componentName, componentDepends, kv)
			if !exist {
				errs = append(errs, fmt.Errorf("Template `%s` refer to unknown substitution `%s`", filename, variable))
				substitution = "(unknown)"
			} else {
				if parameters.RequireExpansion(substitution) {
					util.WarnOnce("Template `%s` substitution `%s` refer to a value `%s` that is not expanded",
						filename, variable, substitution)
				}
			}
			if config.Trace {
				log.Printf("--- %s | %s => %s", variable, componentName, substitution)
			}
			replaced = true
			if encoding != "" {
				if encoding == "base64" {
					substitution = base64.StdEncoding.EncodeToString([]byte(substitution))
				} else if encoding == "unbase64" {
					bytes, err := base64.StdEncoding.DecodeString(substitution)
					if err != nil {
						util.Warn("Unable to decode `%s` base64 value `%s`: %v", variable, util.Trim(substitution), err)
					} else {
						substitution = string(bytes)
					}
				} else {
					util.Warn("Unknown encoding `%s` processing template `%s` substitution `%s`",
						encoding, filename, variable)
				}
			} else {
				substitution = strings.TrimSpace(substitution)
			}
			return substitution
		})

	if !replaced {
		util.Warn("No substitutions found in template `%s`", filename)
	}
	return outContent, errs
}

func addMustacheCompatibleBindings(kv map[string]string) map[string]string {
	for k, v := range kv {
		if strings.Contains(k, ".") {
			kv[strings.ReplaceAll(k, ".", "_")] = v
		}
	}
	return kv
}

func processMustache(content, filename, componentName string, componentDepends []string,
	kv map[string]string) (string, error) {

	template := mustache.New(mustache.SilentMiss(false))
	err := template.ParseString(content)
	if err != nil {
		return "", fmt.Errorf("Unable to parse mustache template `%s`: %v", filename, err)
	}
	outContent, err := template.RenderString(kv)
	if err != nil {
		util.PrintMap(kv)
		return outContent, fmt.Errorf("Unable to render mustache template `%s`: %v", filename, err)
	}
	return outContent, nil
}
