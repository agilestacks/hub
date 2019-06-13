package lifecycle

import (
	"log"
	"os"
	"strings"

	"hub/config"
	"hub/kube"
	"hub/manifest"
	"hub/parameters"
	"hub/util"
)

func captureProvides(component *manifest.ComponentRef, stackBaseDir string, componentsBaseDir string, provides []string,
	componentOutputs parameters.CapturedOutputs) parameters.CapturedOutputs {

	outputs := make(parameters.CapturedOutputs)
	for _, prov := range provides {
		switch prov {
		case "kubernetes":
			kubernetesParams := kube.CaptureKubernetes(component, stackBaseDir, componentsBaseDir, componentOutputs)
			parameters.MergeOutputs(outputs, kubernetesParams)

		default:
		}
	}
	return outputs
}

func mergePlatformProvides(provides map[string][]string, platformProvides []string) {
	platform := "*platform*"
	for _, provide := range platformProvides {
		providers, exist := provides[provide]
		if exist {
			providers = append(providers, platform)
		} else {
			providers = []string{platform}
		}
		provides[provide] = providers
	}
}

func mergeProvides(provides map[string][]string, componentName string, componentProvides []string,
	componentOutputs parameters.CapturedOutputs) {

	for _, prov := range componentProvides {
		switch prov {
		case "kubernetes":
			for _, reqOutput := range []string{"dns.domain"} {
				qName := parameters.OutputQualifiedName(reqOutput, componentName)
				_, exist := componentOutputs[qName]
				if !exist {
					log.Printf("Component `%s` declared to provide `%s` but no `%s` output found",
						componentName, prov, qName)
					log.Print("Outputs:")
					parameters.PrintCapturedOutputs(componentOutputs)
					if !config.Force {
						os.Exit(1)
					}
				}
			}

		default:
		}

		who, exist := provides[prov]
		if !exist {
			who = []string{componentName}
		} else if !util.Contains(who, componentName) { // check because of re-deploy
			if config.Debug {
				log.Printf("`%s` already provides `%s`, but component `%s` also provides `%s`",
					strings.Join(who, ", "), prov, componentName, prov)
			}
			who = append(who, componentName)
		}
		provides[prov] = who
	}
}
