// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package version

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// RunTip runs the "go" tool from the development tree.
func RunTip() {
	log.SetFlags(0)

	root, err := goroot("gotip")
	if err != nil {
		log.Fatalf("gotip: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "download" {
		switch len(os.Args) {
		case 2:
			if err := installTip(root, ""); err != nil {
				log.Fatalf("gotip: %v", err)
			}
		case 3:
			if err := installTip(root, os.Args[2]); err != nil {
				log.Fatalf("gotip: %v", err)
			}
		default:
			log.Fatalf("gotip: usage: gotip download [CL number | branch name]")
		}
		log.Printf("Success. You may now run 'gotip'!")
		os.Exit(0)
	}

	gobin := filepath.Join(root, "bin", "go"+exe())
	if _, err := os.Stat(gobin); err != nil {
		log.Fatalf("gotip: not downloaded. Run 'gotip download' to install to %v", root)
	}

	runGo(root)
}

func installTip(root, target string) error {
	git := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = root
		return cmd.Run()
	}
	gitOutput := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		return cmd.Output()
	}

	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if err := os.MkdirAll(root, 0755); err != nil {
			return fmt.Errorf("failed to create repository: %v", err)
		}
		if err := git("clone", "--depth=1", "https://go.googlesource.com/go", root); err != nil {
			return fmt.Errorf("failed to clone git repository: %v", err)
		}
	}

	// If the argument is a simple decimal number, consider it a CL number.
	// Otherwise, consider it a branch name. If it's missing, fetch master.
	if n, _ := strconv.Atoi(target); n >= 1 && strconv.Itoa(n) == target {
		fmt.Fprintf(os.Stderr, "This will download and execute code from golang.org/cl/%s, continue? [y/n] ", target)
		var answer string
		if fmt.Scanln(&answer); answer != "y" {
			return fmt.Errorf("interrupted")
		}

		// ls-remote outputs a number of lines like:
		// 2621ba2c60d05ec0b9ef37cd71e45047b004cead	refs/changes/37/227037/1
		// 51f2af2be0878e1541d2769bd9d977a7e99db9ab	refs/changes/37/227037/2
		// af1f3b008281c61c54a5d203ffb69334b7af007c	refs/changes/37/227037/3
		// 6a10ebae05ce4b01cb93b73c47bef67c0f5c5f2a	refs/changes/37/227037/meta
		refs, err := gitOutput("ls-remote")
		if err != nil {
			return fmt.Errorf("failed to list remotes: %v", err)
		}
		r := regexp.MustCompile(`refs/changes/\d\d/` + target + `/(\d+)`)
		match := r.FindAllStringSubmatch(string(refs), -1)
		if match == nil {
			return fmt.Errorf("CL %v not found", target)
		}
		var ref string
		var patchSet int
		for _, m := range match {
			ps, _ := strconv.Atoi(m[1])
			if ps > patchSet {
				patchSet = ps
				ref = m[0]
			}
		}
		log.Printf("Fetching CL %v, Patch Set %v...", target, patchSet)
		if err := git("fetch", "origin", ref); err != nil {
			return fmt.Errorf("failed to fetch %s: %v", ref, err)
		}
	} else if target != "" {
		log.Printf("Fetching branch %v...", target)
		ref := "refs/heads/" + target
		if err := git("fetch", "origin", ref); err != nil {
			return fmt.Errorf("failed to fetch %s: %v", ref, err)
		}
	} else {
		log.Printf("Updating the go development tree...")
		if err := git("fetch", "origin", "master"); err != nil {
			return fmt.Errorf("failed to fetch git repository updates: %v", err)
		}
	}

	// Use checkout and a detached HEAD, because it will refuse to overwrite
	// local changes, and warn if commits are being left behind, but will not
	// mind if master is force-pushed upstream.
	if err := git("-c", "advice.detachedHead=false", "checkout", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("failed to checkout git repository: %v", err)
	}
	// It shouldn't be the case, but in practice sometimes binary artifacts
	// generated by earlier Go versions interfere with the build.
	//
	// Ask the user what to do about them if they are not gitignored. They might
	// be artifacts that used to be ignored in previous versions, or precious
	// uncommitted source files.
	if err := git("clean", "-i", "-d"); err != nil {
		return fmt.Errorf("failed to cleanup git repository: %v", err)
	}
	// Wipe away probably boring ignored files without bothering the user.
	if err := git("clean", "-q", "-f", "-d", "-X"); err != nil {
		return fmt.Errorf("failed to cleanup git repository: %v", err)
	}

	cmd := exec.Command(filepath.Join(root, "src", makeScript()))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = filepath.Join(root, "src")
	if runtime.GOOS == "windows" {
		// Workaround make.bat not autodetecting GOROOT_BOOTSTRAP. Issue 28641.
		goroot, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			return fmt.Errorf("failed to detect an existing go installation for bootstrap: %v", err)
		}
		cmd.Env = append(os.Environ(), "GOROOT_BOOTSTRAP="+strings.TrimSpace(string(goroot)))
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build go: %v", err)
	}

	return nil
}

func makeScript() string {
	switch runtime.GOOS {
	case "plan9":
		return "make.rc"
	case "windows":
		return "make.bat"
	default:
		return "make.bash"
	}
}
