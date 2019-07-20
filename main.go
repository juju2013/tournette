package main

import (
  "fmt"
  "io"
	"io/ioutil"
  "os"
  "os/exec"
  "regexp"
  "strconv"
  "strings"
  "time"
  "github.com/radovskyb/watcher"
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
  
  wbuildcmd = getOption("TOURNETTE_BUILDCMD", "go build .")
  wruncmd   = getOption("TOURNETTE_RUNCMD", "") // if empty, we'll try to find the latest changed file in current directory

  wr := getOption("TOURNETTE_REGEX", "^.*\\.go$")
  fmt.Printf("Watching %v\n", wr)
	r := regexp.MustCompile(wr)
	w.AddFilterHook(watcher.RegexFilterHook(r, false))
  woff = false
  go evtHandler()
  
  rebuild()
  
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
  if err := w.Start(time.Millisecond * time.Duration(ww)); err != nil {
    fmt.Printf(err.Error())
    os.Exit(1)
	}
}

func evtHandler() {
  for {
    select {
    case event := <-w.Event:	
      if ! woff {
        fmt.Printf("Change detected:%v, rebuilding...\n",event) // Print the event's info.
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
  
  cmdargs := strings.Split(wbuildcmd," ")
  cmd := runDMC(cmdargs[0], cmdargs[1:]...)
  if cmd != nil {
    if err := cmd.Wait(); err != nil {
      fmt.Printf(err.Error())
    }
  }
  fmt.Printf("End of Build\n")
}

func postbuild() {
  woff = false
  runTarget()
}

func prebuild() {
  woff = true
  killTarget()
}

func runTarget(){
  fmt.Printf("DEBUG:runTarget\n")
  if len(wruncmd) == 0 {
    findTarget()
  }
  if len(wruncmd) == 0 {
    fmt.Printf("No exectuable found")
    return
  }
  if len(os.Args) > 1 {
    wtarget = runDMC(wruncmd, os.Args[1:]...)
  } else {
    wtarget = runDMC(wruncmd)
  }
}

func killTarget() {
  if wtarget == nil {
    return
  }
  if wtarget.Process == nil {
    return
  }
  fmt.Printf("Killing %v\n", wtarget.Process.Pid)
  wtarget.Process.Kill()
}

func findTarget() {
  var ffi os.FileInfo
  files, err := ioutil.ReadDir(".")
  if err != nil {
    fmt.Printf("Cannot access .: %s\n", err)
  }
  for _, fi := range files {
    if fi.Mode() & os.ModeType == 0 { // regular file
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
    wruncmd = "./"+ffi.Name()
  }
}

func runDMC(prog string, args ... string) *exec.Cmd {
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

	if err := cmd.Start(); err != nil {
    fmt.Printf(err.Error())
		return nil
	}

	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)
  
  return cmd
}

