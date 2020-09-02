/*
Copyright 2020 The Kubernetes Authors.

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

package kepctl

import (
	"fmt"
	"regexp"

	"github.com/pkg/errors"

	"k8s.io/enhancements/pkg/kepval/keps"
	"k8s.io/enhancements/pkg/kepval/keps/validations"
)

type QueryOpts struct {
	CommonArgs
	SIG         []string
	Status      []string
	Stage       []string
	PRRApprover []string
	IncludePRs  bool
}

// Validate checks the args and cleans them up if needed
func (c *QueryOpts) Validate(args []string) error {
	if len(c.SIG) > 0 {
		sigs, err := selectByRegexp(validations.Sigs(), c.SIG)
		if err != nil {
			return err
		}
		if len(sigs) == 0 {
			return fmt.Errorf("No SIG matches any of the passed regular expressions")
		}
		c.SIG = sigs
	}
	//TODO: check the valid values of stage, status, etc.
	return nil
}

// Query searches the local repo and possibly GitHub for KEPs
// that match the search criteria.
func (c *Client) Query(opts QueryOpts) error {
	fmt.Fprintf(c.Out, "Searching for KEPs...\n")
	repoPath, err := c.findEnhancementsRepo(opts.CommonArgs)
	if err != nil {
		return errors.Wrap(err, "unable to search KEPs")
	}

	c.SetGitHubToken(opts.CommonArgs)

	var allKEPs []*keps.Proposal
	// load the KEPs for each listed SIG
	for _, sig := range opts.SIG {
		// KEPs in the local filesystem
		names, err := findLocalKEPs(repoPath, sig)
		if err != nil {
			fmt.Fprintf(c.Err, "error searching for local KEPs from %s: %s\n", sig, err)
		}

		for _, k := range names {
			kep, err := c.readKEP(repoPath, sig, k)
			if err != nil {
				fmt.Fprintf(c.Err, "error reading KEP %s: %s\n", k, err)
			} else {
				allKEPs = append(allKEPs, kep)
			}
		}

		// Open PRs; existing KEPs with open PRs will be shown twice
		if opts.IncludePRs {
			prKeps, err := c.findKEPPullRequests(sig)
			if err != nil {
				fmt.Fprintf(c.Err, "error searching for KEP PRs from %s: %s\n", sig, err)
			}
			if prKeps != nil {
				allKEPs = append(allKEPs, prKeps...)
			}
		}
	}

	// filter the KEPs by criteria
	allowedStatus := sliceToMap(opts.Status)
	allowedStage := sliceToMap(opts.Stage)
	allowedPRR := sliceToMap(opts.PRRApprover)

	var keep []*keps.Proposal
	for _, k := range allKEPs {
		if len(opts.Status) > 0 && !allowedStatus[k.Status] {
			continue
		}
		if len(opts.Stage) > 0 && !allowedStage[k.Stage] {
			continue
		}
		if len(opts.PRRApprover) > 0 && !atLeastOne(k.PRRApprovers, allowedPRR) {
			continue
		}
		keep = append(keep, k)
	}

	c.PrintTable(DefaultPrintConfigs("LastUpdated", "Stage", "Status", "SIG", "Authors", "Title", "Link"), keep)
	return nil
}

func sliceToMap(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

// returns all strings in vals that match at least one
// regexp in regexps
func selectByRegexp(vals []string, regexps []string) ([]string, error) {
	var matches []string
	for _, s := range vals {
		for _, r := range regexps {
			found, err := regexp.MatchString(r, s)
			if err != nil {
				return matches, err
			}
			if found {
				matches = append(matches, s)
				break
			}
		}
	}
	return matches, nil
}

// returns true if at least one of vals is in the allowed map
func atLeastOne(vals []string, allowed map[string]bool) bool {
	for _, v := range vals {
		if allowed[v] {
			return true
		}
	}

	return false
}
