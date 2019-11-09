package main

import (
	"fmt"
	"github.com/pborman/getopt/v2"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

func debugf(fmt string, args ...interface{}) {
	log.Printf(fmt, args...)
}

func fatalf(fmt string, args ...interface{}) {
	log.Fatalf("git-subtrac: "+fmt, args...)
}

type Trac struct {
	name       string
	hash       plumbing.Hash
	subHeads   []*Trac
	tracCommit *object.Commit
}

func (t Trac) String() string {
	var heads []string
	for _, v := range t.subHeads {
		heads = append(heads, fmt.Sprintf("%.10v", v.hash))
	}
	headstr := strings.Join(heads, ",")

	if t.tracCommit != nil {
		return fmt.Sprintf("%.10v:%v[%v]<%.10v>",
			t.hash, t.name, headstr, t.tracCommit.Hash)
	} else {
		return fmt.Sprintf("%.10v:%v[%v]<>", t.hash, t.name, headstr)
	}
}

type Cache struct {
	repo  *git.Repository
	tracs map[plumbing.Hash]*Trac
}

func NewCache(r *git.Repository) *Cache {
	c := Cache{
		repo:  r,
		tracs: make(map[plumbing.Hash]*Trac),
	}
	return &c
}

func (c *Cache) String() string {
	var l []*Trac
	for _, v := range c.tracs {
		l = append(l, v)
	}

	sort.Slice(l, func(i, j int) bool {
		return l[i].name < l[j].name
	})

	var out []string
	for _, v := range l {
		out = append(out, v.String())
	}
	return strings.Join(out, "\n")
}

func (c *Cache) tracByRef(refname string) (*Trac, error) {
	h, err := c.repo.ResolveRevision(plumbing.Revision(refname))
	if err != nil {
		return nil, fmt.Errorf("%v: %v", refname, err)
	}
	commit, err := c.repo.CommitObject(*h)
	if err != nil {
		return nil, fmt.Errorf("%v: %v", refname, err)
	}
	return c.tracCommit(refname, commit)
}

// Mercifully, git's content-addressable storage means there are never
// any cycles when traversing the commit+submodule hierarchy, although the
// same sub-objects may occur many times at different points in the tree.
func (c *Cache) tracCommit(path string, commit *object.Commit) (*Trac, error) {
	//	debugf("commit %.10v %v\n", commit.Hash, path)
	trac := c.tracs[commit.Hash]
	if trac != nil {
		//		debugf("   found: %v\n", trac)
		return trac, nil
	}
	trac = &Trac{
		name: path,
		hash: commit.Hash,
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("%v:%.10v: %v", path, commit.Hash, err)
	}
	_, err = c.tracTree(path+"/", tree)
	if err != nil {
		return nil, err
	}
	for i, parent := range commit.ParentHashes {
		pc, err := c.repo.CommitObject(parent)
		if err != nil {
			return nil, fmt.Errorf("%v:%.10v: %v", path, pc.Hash, err)
		}
		np := commitPath(path, i+1)
		_, err = c.tracCommit(np, pc)
		if err != nil {
			return nil, err
		}
	}
	c.add(trac)
	return trac, nil
}

func commitPath(path string, sub int) string {
	if sub != 1 {
		return fmt.Sprintf("%s^%d", path, sub)
	}
	ix := strings.LastIndexByte(path, '~')
	if ix < 0 {
		return fmt.Sprintf("%s~1", path)
	}
	v, err := strconv.Atoi(path[ix+1:])
	if err != nil {
		return fmt.Sprintf("%s~1", path)
	}
	return fmt.Sprintf("%s~%d", path[:ix], v+1)
}

func (c *Cache) tracTree(path string, tree *object.Tree) (*Trac, error) {
	trac := c.tracs[tree.Hash]
	if trac != nil {
		return trac, nil
	}
	for _, e := range tree.Entries {
		if e.Mode == filemode.Submodule {
			sc, err := c.repo.CommitObject(e.Hash)
			subpath := fmt.Sprintf("%s%s@%.10v", path, e.Name, e.Hash)
			if err != nil {
				return nil, fmt.Errorf("%v: %v (maybe fetch it?)",
					subpath, err)
			}
			_, err = c.tracCommit(subpath, sc)
			if err != nil {
				return nil, err
			}
		} else if e.Mode == filemode.Dir {
			t, err := c.repo.TreeObject(e.Hash)
			if err != nil {
				return nil, fmt.Errorf("%v:%.10v: %v",
					path+e.Name, e.Hash, err)
			}
			_, err = c.tracTree(path+e.Name+"/", t)
			if err != nil {
				return nil, err
			}
		}
	}
	trac = &Trac{
		name: path,
		hash: tree.Hash,
	}
	c.add(trac)
	return trac, nil
}

func (c *Cache) add(trac *Trac) {
	debugf("  add %.10v %v\n", trac.hash, trac.name)
	c.tracs[trac.hash] = trac
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
		c := NewCache(r)
		refname := args[1]
		_, err = c.tracByRef(refname)
		if err != nil {
			fatalf("%v\n", err)
		}
		fmt.Printf("%v\n", c)
	default:
		usagef("unknown command %v", args[0])
	}
}
