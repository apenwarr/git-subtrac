package main

import (
	"fmt"
	"github.com/pborman/getopt/v2"
	"gopkg.in/src-d/go-git.v4"
	"log"
	"os"
)

func debugf(fmt string, args ...interface{}) {
	log.Printf(fmt, args...)
}

func fatalf(fmt string, args ...interface{}) {
	log.Fatalf("git-subtrac: "+fmt, args...)
}

var usage_str = `
Usage: %v [-d GIT_DIR] <command>

Commands:
    cid <ref>    Generate a tracking commit id based on the given ref
    update       Update all local branches with a matching *.trac branch
`

func usagef(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, usage_str[1:], os.Args[0])
	fmt.Fprintf(os.Stderr, "\nfatal: "+format+"\n", args...)
	os.Exit(99)
}

func main() {
	log.SetFlags(0)

	repodir := getopt.StringLong("git-dir", 'd', ".", "path to git repo")
	excludes := getopt.ListLong("exclude", 'x', "", "commitids to exclude")
	autoexclude := getopt.BoolLong("auto-exclude", 0, "auto exclude missing commits")
	getopt.Parse()

	r, err := git.PlainOpen(*repodir)
	if err != nil {
		fatalf("git: %v: %v\n", repodir, err)
	}

	args := getopt.Args()
	if len(args) < 1 {
		usagef("no command specified.")
	}

	c := NewCache(*repodir, r, *excludes, *autoexclude, debugf)

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
	default:
		usagef("unknown command %v", args[0])
	}
}
