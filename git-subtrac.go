package main

import (
	"fmt"
	"github.com/pborman/getopt"
	"gopkg.in/src-d/go-git.v4"
	"log"
	"os"
)

func fatalf(fmt string, args ...interface{}) {
	log.Fatalf("git-subtrac: "+fmt, args...)
}

var usage_str = `
Commands:
    cid <ref>       Print the id of a tracking commit based on the given ref
    dump <refs...>  Print the cache after loading the given branch ref(s)
    update          Update all local branches with a matching *.trac branch
`

func usage() {
	fmt.Fprintf(os.Stderr, "\n")
	getopt.PrintUsage(os.Stderr)
	fmt.Fprintf(os.Stderr, usage_str)
}

func usagef(format string, args ...interface{}) {
	usage()
	fmt.Fprintf(os.Stderr, "\nfatal: "+format+"\n", args...)
	os.Exit(99)
}

func main() {
	log.SetFlags(0)
	infof := log.Printf

	getopt.SetUsage(usage)
	repodir := getopt.StringLong("git-dir", 'd', ".", "path to git repo", "GIT_DIR")
	excludes := getopt.ListLong("exclude", 'x', "commitids to exclude", "commitids...")
	autoexclude := getopt.BoolLong("auto-exclude", 0, "auto exclude missing commits")
	verbose := getopt.BoolLong("verbose", 'v', "verbose mode")
	getopt.Parse()

	r, err := git.PlainOpen(*repodir)
	if err != nil {
		fatalf("git: %v: %v\n", repodir, err)
	}

	args := getopt.Args()
	if len(args) < 1 {
		usagef("no command specified.")
	}

	var debugf func(fmt string, args ...interface{})
	if *verbose {
		debugf = infof
	} else {
		debugf = func(fmt string, args ...interface{}) {}
	}

	c, err := NewCache(*repodir, r, *excludes, *autoexclude, debugf, infof)
	if err != nil {
		fatalf("NewCache: %v\n", err)
	}

	switch args[0] {
	case "update":
		if len(args) != 1 {
			usagef("command 'update' takes no arguments")
		}
		err := c.UpdateBranchRefs()
		if err != nil {
			fatalf("%v\n", err)
		}
	case "cid":
		if len(args) != 2 {
			usagef("command 'cid' takes exactly 1 argument")
		}
		refname := args[1]
		trac, err := c.TracByRef(refname)
		if err != nil {
			fatalf("%v\n", err)
		}
		fmt.Printf("%v\n", trac.Hash)
	case "dump":
		if len(args) < 2 {
			usagef("command 'dump' takes at least 1 argument")
		}
		for _, refname := range args[1:] {
			_, err := c.TracByRef(refname)
			if err != nil {
				fatalf("%v\n", err)
			}
		}
		fmt.Printf("%v\n", c)
	default:
		usagef("unknown command %v", args[0])
	}
}
