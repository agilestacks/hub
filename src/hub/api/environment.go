package api

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
)

const environmentsResource = "hub/api/v1/environments"

var environmentsCache = make(map[string]*Environment)

func Environments(selector string, showSecrets, showMyTeams,
	showServiceAccount, showServiceAccountLoginToken, getCloudTemporaryCredentials bool) {

	envs, err := environmentsBy(selector)
	if err != nil {
		log.Fatalf("Unable to query for Environment(s): %v", err)
	}
	if len(envs) == 0 {
		fmt.Print("No Environments\n")
	} else {
		fmt.Print("Environments:\n")
		errors := make([]error, 0)
		for _, env := range envs {
			title := fmt.Sprintf("%s [%s]", env.Name, env.Id)
			if env.Description != "" {
				title = fmt.Sprintf("%s - %s", title, env.Description)
			}
			fmt.Printf("\n\t%s\n", title)
			if len(env.Tags) > 0 {
				fmt.Printf("\t\tTags: %s\n", strings.Join(env.Tags, ", "))
			}
			if env.CloudAccount != "" {
				account, err := cloudAccountById(env.CloudAccount)
				if err != nil {
					errors = append(errors, err)
				} else {
					fmt.Printf("\t\tCloud Account: %s\n", formatCloudAccount(account))
				}
				if getCloudTemporaryCredentials {
					keys, err := cloudAccountTemporaryCredentials(env.CloudAccount)
					if err != nil {
						errors = append(errors, err)
					} else {
						fmt.Printf("\t\tTemporary Security Credentials: %s\n", formatCloudAccountTemporaryCredentials(keys))
					}
				}
			}
			if len(env.Parameters) > 0 {
				fmt.Print("\t\tParameters:\n")
			}
			resource := fmt.Sprintf("%s/%s", environmentsResource, env.Id)
			for _, param := range sortParameters(env.Parameters) {
				formatted, err := formatParameter(resource, param, showSecrets)
				fmt.Printf("\t\t%s\n", formatted)
				if err != nil {
					errors = append(errors, err)
				}
			}
			if len(env.TeamsPermissions) > 0 {
				formatted := formatTeams(env.TeamsPermissions)
				fmt.Printf("\t\tTeams: %s\n", formatted)
				if showMyTeams {
					teams, err := myTeams(env.Id)
					formatted := formatTeams(teams)
					fmt.Printf("\t\tMy Teams: %s\n", formatted)
					if err != nil {
						errors = append(errors, err)
					}
				}
			}
			if showServiceAccount {
				teams, err := myTeams(env.Id)
				if err != nil {
					errors = append(errors, err)
				} else {
					if len(teams) > 0 {
						for _, team := range teams {
							account, err := serviceAccount(env.Id, team.Id)
							if err != nil {
								errors = append(errors, err)
							} else {
								formatted := formatServiceAccount(team, account, showServiceAccountLoginToken)
								fmt.Printf("\t\tService Account: %s\n", formatted)
							}
						}
					}
				}
			}
		}
		if len(errors) > 0 {
			fmt.Print("Errors encountered:\n")
			for _, err := range errors {
				fmt.Printf("\t%v\n", err)
			}
		}
	}
}

func cachedEnvironmentBy(selector string) (*Environment, error) {
	env, cached := environmentsCache[selector]
	if !cached {
		var err error
		env, err = environmentBy(selector)
		if err != nil {
			return nil, err
		}
		environmentsCache[selector] = env
	}
	return env, nil
}

func environmentBy(selector string) (*Environment, error) {
	_, err := strconv.ParseUint(selector, 10, 32)
	if err != nil {
		return environmentByName(selector)
	}
	return environmentById(selector)
}

func environmentsBy(selector string) ([]Environment, error) {
	_, err := strconv.ParseUint(selector, 10, 32)
	if err != nil {
		return environmentsByName(selector)
	}
	environment, err := environmentById(selector)
	if err != nil {
		return nil, err
	}
	if environment != nil {
		return []Environment{*environment}, nil
	}
	return nil, nil
}

func environmentById(id string) (*Environment, error) {
	path := fmt.Sprintf("%s/%s", environmentsResource, url.PathEscape(id))
	var jsResp Environment
	code, err := get(hubApi, path, &jsResp)
	if code == 404 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Error querying Hub Service Environments: %v", err)
	}
	if code != 200 {
		return nil, fmt.Errorf("Got %d HTTP querying Hub Service Environments, expected 200 HTTP", code)
	}
	return &jsResp, nil
}

func environmentByName(name string) (*Environment, error) {
	environments, err := environmentsByName(name)
	if err != nil {
		return nil, fmt.Errorf("Unable to query for Environment `%s`: %v", name, err)
	}
	if len(environments) == 0 {
		return nil, fmt.Errorf("No Environment `%s` found", name)
	}
	if len(environments) > 1 {
		return nil, fmt.Errorf("More than one Environment returned by name `%s`", name)
	}
	environment := environments[0]
	return &environment, nil
}

func environmentsByName(name string) ([]Environment, error) {
	path := environmentsResource
	if name != "" {
		path += "?name=" + url.QueryEscape(name)
	}
	var jsResp []Environment
	code, err := get(hubApi, path, &jsResp)
	if code == 404 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Error querying Hub Service Environments: %v", err)
	}
	if code != 200 {
		return nil, fmt.Errorf("Got %d HTTP querying Hub Service Environments, expected 200 HTTP", code)
	}
	return jsResp, nil
}

func formatEnvironmentRef(ref *EnvironmentRef) string {
	return fmt.Sprintf("%s [%s]", ref.Name, ref.Id)
}

func myTeams(environmentId string) ([]Team, error) {
	path := fmt.Sprintf("%s/%s/my-teams", environmentsResource, url.PathEscape(environmentId))
	var jsResp []Team
	code, err := get(hubApi, path, &jsResp)
	if err != nil {
		return nil, fmt.Errorf("Error querying Hub Service Environment `%s` My Teams: %v",
			environmentId, err)
	}
	if code != 200 {
		return nil, fmt.Errorf("Got %d HTTP querying Hub Service Environment `%s` My Teams, expected 200 HTTP",
			code, environmentId)
	}
	return jsResp, nil
}

func formatTeams(teams []Team) string {
	formatted := make([]string, 0, len(teams))
	for _, team := range teams {
		formatted = append(formatted, fmt.Sprintf("%s (%s)", team.Name, team.Role))
	}
	return strings.Join(formatted, ", ")
}

func serviceAccount(environmentId, teamId string) (*ServiceAccount, error) {
	path := fmt.Sprintf("%s/%s/service-account/%s", environmentsResource, url.PathEscape(environmentId), url.PathEscape(teamId))
	var jsResp ServiceAccount
	code, err := get(hubApi, path, &jsResp)
	if err != nil {
		return nil, fmt.Errorf("Error querying Hub Service Team `%s` Service Account: %v",
			teamId, err)
	}
	if code != 200 {
		return nil, fmt.Errorf("Got %d HTTP querying Hub Service Team `%s` Service Account, expected 200 HTTP",
			code, teamId)
	}
	return &jsResp, nil
}

func formatServiceAccount(team Team, account *ServiceAccount, showLoginToken bool) string {
	formatted := fmt.Sprintf("%s (%s) %s", team.Name, team.Role, account.Name)
	if showLoginToken {
		formatted = fmt.Sprintf("%s\n\t\t\tLogin Token: %s", formatted, account.LoginToken)
	}
	return formatted
}
