//(yet another) simple utility to rebuild/reload your Go program when source changes
// Copyright 2019 by juju2013@github
package main

import (
	"fmt"
	"github.com/radovskyb/watcher"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	w         *watcher.Watcher
	woff      bool
	wbuildcmd string
	wruncmd   string
	wtarget   *exec.Cmd
)

// get opt via os.Env if set, or defval if not
func getOption(opt string, defval string) (r string) {
	r = os.Getenv(opt)
	if len(r) == 0 {
		r = defval
	}
	return
}

func main() {
	fmt.Printf("Tournette\n")
	w = watcher.New()
	w.SetMaxEvents(1)
	w.FilterOps(watcher.Rename, watcher.Move, watcher.Create, watcher.Remove, watcher.Write)

	// Command to build your program, go build . if not specified
	wbuildcmd = getOption("TOURNETTE_BUILDCMD", "go build .")
	// Command to run your program, empty if not specified
	wruncmd = getOption("TOURNETTE_RUNCMD", "") // if empty, we'll try to find the latest changed executable in current directory
	// Filter for source file to watch
	wr := getOption("TOURNETTE_REGEX", "^.*\\.go$")
	fmt.Printf("Watching %v\n", wr)
	r := regexp.MustCompile(wr)
	w.AddFilterHook(watcher.RegexFilterHook(r, false))
	woff = false
	go evtHandler()

	// Start by rebuild - and launch - your program
	rebuild()

	// The root directory for source to watch
	wd := getOption("TOURNETTE_DIR", ".")
	if err := w.AddRecursive(wd); err != nil {
		fmt.Printf(err.Error())
		os.Exit(1)
	}

	ww, err := strconv.ParseInt(getOption("TOURNETTE_INTERVAL", "1000"), 10, 64)
	if err != nil {
		fmt.Printf(err.Error())
		os.Exit(1)
	}
	// start watch source change, will loop
	if err := w.Start(time.Millisecond * time.Duration(ww)); err != nil {
		fmt.Printf(err.Error())
		os.Exit(1)
	}
}

// Handle watch events such as file change, error or exit
func evtHandler() {
	for {
		select {
		case event := <-w.Event:
			if !woff {
				fmt.Printf("Change detected:%v, rebuilding...\n", event) // Print the event's info.
				go rebuild()
			}
		case err := <-w.Error:
			fmt.Printf(err.Error())
		case <-w.Closed:
			return
		}
	}
}

// rebuild from current directory
func rebuild() {
	prebuild()
	defer postbuild()

	cmdargs := strings.Split(wbuildcmd, " ")
	cmd := runDMC(cmdargs[0], cmdargs[1:]...)
	if cmd != nil {
		if err := cmd.Wait(); err != nil {
			fmt.Printf(err.Error())
		}
	}
	fmt.Printf("End of Build\n")
}

// things to do before rebuild, disable file watch and kill the program
func prebuild() {
	woff = true
	killTarget()
}

// things to do after rebuild, enable file watch and run the compiled program
func postbuild() {
	woff = false
	runTarget()
}

// run the compiled program
func runTarget() {
	fmt.Printf("DEBUG:runTarget\n")
	if len(wruncmd) == 0 {
		findTarget()
	}
	if len(wruncmd) == 0 {
		fmt.Printf("No exectuable found")
		return
	}
	cmdargs := strings.Split(wruncmd, " ")
	cmdargs = append(cmdargs, "")
	if len(os.Args) > 1 {
		cmdargs = append(cmdargs, os.Args[1:]...)
	}
	wtarget = runDMC(cmdargs[0], cmdargs[1:]...)
}

// kill the program launched by runTarget()
func killTarget() {
	if wtarget == nil {
		return
	}
	if wtarget.Process == nil {
		return
	}
	fmt.Printf("Killing %v\n", wtarget.Process.Pid)

	pgid, err := syscall.Getpgid(wtarget.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, 15)
	}

	wtarget.Process.Wait()
}

// find the target program to launch, if not specified by TOURNETTE_RUNCMD
func findTarget() {
	var ffi os.FileInfo
	files, err := ioutil.ReadDir(".")
	if err != nil {
		fmt.Printf("Cannot access .: %s\n", err)
	}
	for _, fi := range files {
		if fi.Mode()&os.ModeType == 0 { // regular file
			if (fi.Mode() & 0111) != 0 { // executable
				if ffi != nil {
					if fi.ModTime().After(ffi.ModTime()) { // more recent file
						ffi = fi
					}
				} else {
					ffi = fi
				}
			}
		}
	}
	if ffi != nil {
		wruncmd = "./" + ffi.Name()
	}
}

// Start a program, pipe stderr/stdout to parent stdout
func runDMC(prog string, args ...string) *exec.Cmd {
	fmt.Printf("Running this way: %v %v\n", prog, args)
	cmd := exec.Command(prog, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Printf(err.Error())
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf(err.Error())
		return nil
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf(err.Error())
		return nil
	}

	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	return cmd
}
