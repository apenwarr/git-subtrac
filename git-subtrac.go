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
	getopt.Parse()

	r, err := git.PlainOpen(*repodir)
	if err != nil {
		fatalf("git: %v: %v\n", repodir, err)
	}

	args := getopt.Args()
	if len(args) < 1 {
		usagef("no command specified.")
	}

	switch args[0] {
	case "cid":
		if len(args) != 2 {
			usagef("command cid takes exactly 1 argument")
		}
		c := NewCache(*repodir, r, *excludes, debugf)
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
