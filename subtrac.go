package main

import (
	"fmt"
	//	"github.com/pborman/getopt/v2"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"log"
	"strings"
)

func debugf(fmt string, args ...interface{}) {
	log.Printf(fmt, args...)
}

func fatalf(fmt string, args ...interface{}) {
	log.Fatalf("git-subtrac: "+fmt, args...)
}

type Trac struct {
	commit     object.Commit
	subHeads   []Trac
	tracCommit *object.Commit
}

func (t Trac) String() string {
	var heads []string
	for _, v := range t.subHeads {
		heads = append(heads, fmt.Sprintf("%.10v", v.commit.Hash))
	}
	headstr := strings.Join(heads, ",")

	if t.tracCommit != nil {
		return fmt.Sprintf("%.10v[%v]<%.10v>",
			t.commit.Hash, headstr, t.tracCommit.Hash)
	} else {
		return fmt.Sprintf("%.10v[%v]<>", t.commit.Hash, headstr)
	}
}

type Cache struct {
	repo  *git.Repository
	tracs map[plumbing.Hash]Trac
}

func NewCache(r *git.Repository) *Cache {
	c := Cache{
		repo:  r,
		tracs: make(map[plumbing.Hash]Trac),
	}
	return &c
}

func (c *Cache) String() string {
	var out []string
	for _, v := range c.tracs {
		out = append(out, v.String())
	}
	return strings.Join(out, "\n")
}

// Mercifully, git's content-addressable storage means there are never
// any cycles when traversing the commit+submodule hierarchy, although the
// same sub-objects may occur many times at different points in the tree.
func (c *Cache) Add(commit *object.Commit) error {
	c.tracs[commit.Hash] = Trac{
		commit: *commit,
	}
	return nil
}

func (c *Cache) AddByRef(refname string) error {
	rn := plumbing.NewBranchReferenceName(refname)
	ref, err := c.repo.Reference(rn, true)
	if err != nil {
		return err
	}
	commit, err := c.repo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	return c.Add(commit)
}

func main() {
	log.SetFlags(0)
	r, err := git.PlainOpen(".")
	if err != nil {
		fatalf("git.PlainOpen: %v\n", err)
	}
	c := NewCache(r)
	refname := "junk"
	err = c.AddByRef(refname)
	if err != nil {
		fatalf("AddByRef: %v: %v\n", refname, err)
	}
	debugf("cache:\n%v\n", c)
}
