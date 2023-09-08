// Copyright 2023 The Shac Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Validate verifies a shac.textproto document is valid.
func (doc *Document) Validate() error {
	if doc.MinShacVersion != "" {
		v := parseVersion(doc.MinShacVersion)
		if v == nil || len(v) > len(Version) {
			return errors.New("min_shac_version is invalid")
		}
		for i := range v {
			if v[i] > Version[i] {
				return fmt.Errorf("min_shac_version specifies unsupported version %q, running %d.%d.%d", doc.MinShacVersion, Version[0], Version[1], Version[2])
			}
			if v[i] < Version[i] {
				break
			}
		}
	}
	deps := map[string]string{}
	aliases := map[string]struct{}{}
	if doc.Requirements != nil {
		if len(doc.Requirements.Indirect) > 0 && len(doc.Requirements.Direct) == 0 {
			return errors.New("cannot have indirect dependency without direct one")
		}
		for i, d := range doc.Requirements.Direct {
			if err := d.Validate(); err != nil {
				return fmt.Errorf("direct require block #%d: %w", i+1, err)
			}
			if _, ok := deps[d.Url]; ok {
				return fmt.Errorf("direct require block #%d: %s was already listed", i+1, d.Url)
			}
			deps[d.Url] = d.Version
			if d.Alias != "" {
				if _, ok := aliases[d.Alias]; ok {
					return fmt.Errorf("direct require block #%d: alias %s was already listed", i+1, d.Alias)
				}
				aliases[d.Alias] = struct{}{}
			}
		}
		for i, d := range doc.Requirements.Indirect {
			if err := d.Validate(); err != nil {
				return fmt.Errorf("indirect require block #%d: %w", i+1, err)
			}
			if _, ok := deps[d.Url]; ok {
				return fmt.Errorf("indirect require block #%d: %s was already listed", i+1, d.Url)
			}
			deps[d.Url] = d.Version
			if d.Alias != "" {
				if _, ok := aliases[d.Alias]; ok {
					return fmt.Errorf("indirect require block #%d: alias %s was already listed", i+1, d.Alias)
				}
				aliases[d.Alias] = struct{}{}
			}
		}
	}
	seen := map[string][]*VersionDigest{}
	if doc.Sum != nil {
		if len(deps) == 0 && len(doc.Sum.Known) > 0 {
			return errors.New("cannot have sum without at least one dependency")
		}
		for i, k := range doc.Sum.Known {
			if err := k.Validate(); err != nil {
				return fmt.Errorf("sum known block #%d: %w", i+1, err)
			}
			if _, ok := seen[k.Url]; ok {
				return fmt.Errorf("sum known block #%d: %s was already listed", i+1, k.Url)
			}
			seen[k.Url] = k.Seen
		}
	}
	// Make sure seen is a super set of deps.
	for name, version := range deps {
		known, ok := seen[name]
		if !ok {
			return fmt.Errorf("dependency %s doesn't have a known block", name)
		}
		found := false
		for _, vd := range known {
			if vd.Version == version {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("dependency %s doesn't have a known version %s", name, version)
		}
	}
	if doc.VendorPath != "" {
		// Make sure the path exists. We currently allow paths outside the root,
		// since it's useful for local testing. This will fail to load them
		// elsewhere.
		if path.Clean(doc.VendorPath) != doc.VendorPath {
			return fmt.Errorf("vendor_path %s is not clean", doc.VendorPath)
		}
	}
	return nil
}

// Validate verifies a shac.textproto require block is valid.
//
// It allows fetching from a Gerrit pending CL or a GitHub pending PR.
//
// For Gerrit, it is guaranteed to be reproducible. For GitHub, ¯\_(ツ)_/¯.
func (d *Dependency) Validate() error {
	if d.Url == "" {
		return errors.New("url must be set")
	}
	if _, err := cleanURL(d.Url); err != nil {
		return fmt.Errorf("url is invalid: %w", err)
	}
	if isBadAlias(d.Alias) {
		return errors.New("alias is invalid")
	}
	if d.Version == "" {
		return errors.New("version must be set")
	}
	// Is it a GitHub PR?
	if ok, _ := regexp.MatchString("^pull/^\\d+/head$", d.Version); !ok {
		// Is it a Gerrit CL?
		if ok, _ := regexp.MatchString("^refs/changes/\\d{1,2}/\\d{1,11}/\\d{1,3}$", d.Version); !ok {
			// Is it a hash?
			if ok, _ := regexp.MatchString("^[a-fA-F0-9]{40,64}$", d.Version); !ok {
				// Version is hopefully a git tag. This will be confirmed at checkout
				// time. Technically, git tags *are* mutable, what we do is to confirm
				// that the hash of the content is the same.
				//
				// https://git-scm.com/docs/git-check-ref-format contains the full specification.
				// Only do a minimal verification so people cannot do nasty stuff.
				if isBadVersion(d.Version) {
					return errors.New("version is invalid")
				}
			}
		}
	}
	return nil
}

// Validate verifies a shac.textproto sum known block is valid.
func (k *Known) Validate() error {
	if k.Url == "" {
		return errors.New("url must be set")
	}
	if _, err := cleanURL(k.Url); err != nil {
		return fmt.Errorf("url is invalid: %w", err)
	}
	if len(k.Seen) == 0 {
		return errors.New("there must be at least on seen entry")
	}
	l := ""
	for i, vd := range k.Seen {
		if vd.Version == "" {
			return fmt.Errorf("seen block #%d: version must be set", i+1)
		}
		if isBadVersion(vd.Version) {
			return fmt.Errorf("seen block #%d: version is invalid", i+1)
		}
		if l >= vd.Version {
			return fmt.Errorf("seen block #%d: version must be sorted", i+1)
		}
		l = vd.Version
		if vd.Digest == "" {
			return fmt.Errorf("seen block #%d: digest must be set", i+1)
		}
		if !strings.HasPrefix(vd.Digest, "h1:") {
			return fmt.Errorf("seen block #%d: digest is invalid, must start with \"h1:\"", i+1)
		}
		dec, err := base64.StdEncoding.DecodeString(vd.Digest[3:])
		if err != nil {
			return fmt.Errorf("seen block #%d: digest is invalid, %w", i+1, err)
		}
		if len(dec) != 32 {
			return fmt.Errorf("seen block #%d: digest is invalid, expected 32 bytes, got %d", i+1, len(dec))
		}
	}
	return nil
}

// Digest returns the digest for the specified url and version.
func (s *Sum) Digest(url, version string) string {
	for _, k := range s.Known {
		if k.Url == url {
			for _, vd := range k.Seen {
				if vd.Version == version {
					return vd.Digest
				}
			}
		}
	}
	// Not found.
	return ""
}

// cleanURL converts a schemaless URI to a fully qualified URI. For now
// assumes HTTPS.
func cleanURL(n string) (string, error) {
	u, err := url.Parse(n)
	if err != nil {
		return "", err
	}
	if u.Scheme != "" {
		return "", fmt.Errorf("unexpected scheme for %s", n)
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("unexpected query for %s", n)
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("unexpected fragment for %s", n)
	}
	if n != u.String() {
		return "", fmt.Errorf("unclean url %s", n)
	}
	u.Scheme = "https"
	// Reparse again to ensure Host and Path are correctly set.
	if u, err = url.Parse(u.String()); err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("a hostname is required; %#v", u)
	}
	if u.Path == "" {
		return "", errors.New("a path is required")
	}
	return u.String(), nil
}

// isBadAlias return true if the alias string looks invalid.
func isBadAlias(s string) bool {
	return strings.ContainsAny(s, "$^[]{}\"'\\:+*<>=.")
}

// isBadVersion return true if the version string looks invalid.
func isBadVersion(s string) bool {
	return strings.ContainsAny(s, "$^[]{}\"'\\:+*<>=") || strings.Contains(s, "..")
}
