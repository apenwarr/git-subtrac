package main

import (
	"fmt"
	"log"
	"os"

	"github.com/apenwarr/git-subtrac/subtrac"
	"github.com/go-git/go-git/v5"
	"github.com/pborman/getopt"
)

func fatalf(format string, args ...any) {
	log.Fatalf("git-subtrac: "+format, args...)
}

var usageText = `
Commands (as "git subtrac ..." or "go tool git-subtrac ..."):
    cid <ref>       Print the id of a tracking commit based on the given ref
    dump <refs...>  Print the cache after loading the given branch ref(s)
    update          Update all local branches with a matching *.trac branch
`

func usage() {
	fmt.Fprintf(os.Stderr, "\n")
	getopt.PrintUsage(os.Stderr)
	fmt.Fprint(os.Stderr, usageText)
}

func usagef(format string, args ...any) {
	usage()
	fmt.Fprintf(os.Stderr, "\nfatal: "+format+"\n", args...)
	os.Exit(99)
}

func main() {
	log.SetFlags(0)
	infof := log.Printf

	getopt.SetUsage(usage)
	help := getopt.BoolLong("help", 'h', "show this help")
	repodir := getopt.StringLong("git-dir", 'd', ".", "path to git repo", "GIT_DIR")
	excludes := getopt.ListLong("exclude", 'x', "commitids to exclude", "commitids...")
	autoexclude := getopt.BoolLong("auto-exclude", 0, "auto exclude missing commits")
	verbose := getopt.BoolLong("verbose", 'v', "verbose mode")
	getopt.Parse()
	if *help {
		usage()
		return
	}

	args := getopt.Args()
	if len(args) < 1 {
		usagef("no command specified.")
	}

	var debugf func(format string, args ...any)
	if *verbose {
		debugf = infof
	} else {
		debugf = func(format string, args ...any) {}
	}

	setupOrFatal := func() *subtrac.Cache {
		r, err := git.PlainOpen(*repodir)
		if err != nil {
			fatalf("git: %v: %v\n", *repodir, err)
		}

		c, err := subtrac.NewCache(*repodir, r, *excludes, *autoexclude, debugf, infof)
		if err != nil {
			fatalf("NewCache: %v\n", err)
		}
		return c
	}

	switch args[0] {
	case "update":
		if len(args) != 1 {
			usagef("command 'update' takes no arguments")
		}
		c := setupOrFatal()
		err := c.UpdateBranchRefs()
		if err != nil {
			fatalf("%v\n", err)
		}
	case "cid":
		if len(args) != 2 {
			usagef("command 'cid' takes exactly 1 argument")
		}
		c := setupOrFatal()
		refname := args[1]
		trac, err := c.TracByRef(refname)
		if err != nil {
			fatalf("%v\n", err)
		}
		if trac != nil {
			fmt.Printf("%v\n", trac.Hash)
		}
	case "dump":
		if len(args) < 2 {
			usagef("command 'dump' takes at least 1 argument")
		}
		c := setupOrFatal()
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
