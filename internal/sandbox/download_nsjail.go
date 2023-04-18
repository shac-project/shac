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

//go:build ignore

// Package main downloads prebuilt nsjail executables from CIPD.
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	nsjailVersion    = "version:2@3.3.chromium.1"
	nsjailCIPDPrefix = "infra/3pp/tools/nsjail/"
	cipdURLTemplate  = "https://chrome-infra-packages.appspot.com/dl/%s/+/" + nsjailVersion
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		log.Fatal(err)
	}
	for _, platform := range [...]string{"linux-amd64", "linux-arm64"} {
		url := fmt.Sprintf(cipdURLTemplate, nsjailCIPDPrefix+platform)
		path := filepath.Join(cwd, fmt.Sprintf("nsjail-%s", platform))
		tempDir, err := os.MkdirTemp("", "platform")
		if err != nil {
			log.Fatal(err)
		}

		err = downloadNsjail(url, path, tempDir)
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			if err == nil {
				err = rmErr
			}
		}
		if err != nil {
			log.Fatal(err)
		}
	}
}

func downloadNsjail(url, path, tempDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	// In case we return an error before the Close() call below.
	defer resp.Body.Close()

	archivePath := filepath.Join(tempDir, "sandbox.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(archive, resp.Body); err != nil {
		return err
	}

	if err := archive.Close(); err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}

	for _, fi := range zr.File {
		if fi.Name != "nsjail" {
			continue
		}
		src, err := fi.Open()
		if err != nil {
			return err
		}

		dest, err := os.Create(path)
		if err != nil {
			return err
		}
		defer dest.Close()

		if err := dest.Chmod(0o777); err != nil {
			return err
		}

		_, err = io.Copy(dest, src)
		return err
	}

	return fmt.Errorf("no nsjail executable found in CIPD package")
}
