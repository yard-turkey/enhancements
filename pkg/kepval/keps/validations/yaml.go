/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validations

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type KeyMustBeSpecified struct {
	key interface{}
}

func (k *KeyMustBeSpecified) Error() string {
	return fmt.Sprintf("missing key %[1]v", k.key)
}

type KeyMustBeString struct {
	key interface{}
}

func (k *KeyMustBeString) Error() string {
	return fmt.Sprintf("key %[1]v must be a string but it is a %[1]T", k.key)
}

type ValueMustBeString struct {
	key   string
	value interface{}
}

func (v *ValueMustBeString) Error() string {
	return fmt.Sprintf("%q must be a string but it is a %T: %v", v.key, v.value, v.value)
}

type ValueMustBeOneOf struct {
	key    string
	value  string
	values []string
}

func (v *ValueMustBeOneOf) Error() string {
	return fmt.Sprintf("%q must be one of (%s) but it is a %T: %v", v.key, strings.Join(v.values, ","), v.value, v.value)
}

type ValueMustBeListOfStrings struct {
	key   string
	value interface{}
}

func (v *ValueMustBeListOfStrings) Error() string {
	return fmt.Sprintf("%q must be a list of strings: %v", v.key, v.value)
}

type MustHaveOneValue struct {
	key string
}

func (m *MustHaveOneValue) Error() string {
	return fmt.Sprintf("%q must have a value", m.key)
}

type MustHaveAtLeastOneValue struct {
	key string
}

func (m *MustHaveAtLeastOneValue) Error() string {
	return fmt.Sprintf("%q must have at least one value", m.key)
}

var (
	listGroups   []string
	prrApprovers []string
)

func Sigs() []string {
	return listGroups
}

func init() {
	resp, err := http.Get("https://raw.githubusercontent.com/kubernetes/community/master/sigs.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch list of sigs: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "invalid status code when fetching list of sigs: %d\n", resp.StatusCode)
		os.Exit(1)
	}
	re := regexp.MustCompile(`- dir: (.*)$`)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		match := re.FindStringSubmatch(scanner.Text())
		if len(match) > 0 {
			listGroups = append(listGroups, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to scan list of sigs: %v\n", err)
		os.Exit(1)
	}
	sort.Strings(listGroups)

	resp, err = http.Get("https://raw.githubusercontent.com/kubernetes/enhancements/master/OWNERS_ALIASES")
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch list of aliases: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "invalid status code when fetching list of aliases: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to read aliases content: %v\n", err)
		os.Exit(1)
	}
	config := &struct {
		Data map[string][]string `json:"aliases,omitempty"`
	}{}
	if err := yaml.Unmarshal(body, config); err != nil {
		fmt.Fprintf(os.Stderr, "unable to read parse aliases content: %v\n", err)
		os.Exit(1)
	}
	for _, approver := range config.Data["prod-readiness-approvers"] {
		prrApprovers = append(prrApprovers, approver)
	}
	sort.Strings(listGroups)
}

var (
	mandatoryKeys = []string{"title", "owning-sig"}
	statuses      = []string{"provisional", "implementable", "implemented", "deferred", "rejected", "withdrawn", "replaced"}
	reStatus      = regexp.MustCompile(strings.Join(statuses, "|"))
	stages        = []string{"alpha", "beta", "stable"}
	reStages      = regexp.MustCompile(strings.Join(stages, "|"))
)

func ValidateStructure(parsed map[interface{}]interface{}) error {
	for _, key := range mandatoryKeys {
		if _, found := parsed[key]; !found {
			return &KeyMustBeSpecified{key}
		}
	}

	for key, value := range parsed {
		// First off the key has to be a string. fact.
		k, ok := key.(string)
		if !ok {
			return &KeyMustBeString{k}
		}
		empty := value == nil

		// figure out the types
		switch strings.ToLower(k) {
		case "status":
			switch v := value.(type) {
			case []interface{}:
				return &ValueMustBeString{k, v}
			}
			v, _ := value.(string)
			if !reStatus.Match([]byte(v)) {
				return &ValueMustBeOneOf{k, v, statuses}
			}
		case "stage":
			switch v := value.(type) {
			case []interface{}:
				return &ValueMustBeString{k, v}
			}
			v, _ := value.(string)
			if !reStages.Match([]byte(v)) {
				return &ValueMustBeOneOf{k, v, stages}
			}
		case "owning-sig":
			switch v := value.(type) {
			case []interface{}:
				return &ValueMustBeString{k, v}
			}
			v, _ := value.(string)
			index := sort.SearchStrings(listGroups, v)
			if index >= len(listGroups) || listGroups[index] != v {
				return &ValueMustBeOneOf{k, v, listGroups}
			}
		// optional strings
		case "editor":
			if empty {
				continue
			}
			fallthrough
		case "title", "creation-date", "last-updated":
			switch v := value.(type) {
			case []interface{}:
				return &ValueMustBeString{k, v}
			}
			v, ok := value.(string)
			if ok && v == "" {
				return &MustHaveOneValue{k}
			}
			if !ok {
				return &ValueMustBeString{k, v}
			}
		// These are optional lists, so skip if there is no value
		case "participating-sigs", "replaces", "superseded-by", "see-also":
			if empty {
				continue
			}
			switch v := value.(type) {
			case []interface{}:
				if len(v) == 0 {
					continue
				}
			case interface{}:
				// This indicates an empty item is valid
				continue
			}
			fallthrough
		case "authors", "reviewers", "approvers":
			switch values := value.(type) {
			case []interface{}:
				if len(values) == 0 {
					return &MustHaveAtLeastOneValue{k}
				}
				if strings.ToLower(k) == "participating-sigs" {
					for _, value := range values {
						v := value.(string)
						index := sort.SearchStrings(listGroups, v)
						if index >= len(listGroups) || listGroups[index] != v {
							return &ValueMustBeOneOf{k, v, listGroups}
						}
					}
				}
			case interface{}:
				return &ValueMustBeListOfStrings{k, values}
			}
		case "prr-approvers":
			switch values := value.(type) {
			case []interface{}:
				// prrApprovers must be sorted to use SearchStrings down below...
				sort.Strings(prrApprovers)
				for _, value := range values {
					v := value.(string)
					if len(v) > 0 && v[0] == '@' {
						// If "@" is appeneded at the beginning, remove it.
						v = v[1:]
					}

					index := sort.SearchStrings(prrApprovers, v)
					if index >= len(prrApprovers) || prrApprovers[index] != v {
						return &ValueMustBeOneOf{k, v, prrApprovers}
					}
				}
			case interface{}:
				return &ValueMustBeListOfStrings{k, values}
			}
		}
	}
	return nil
}
