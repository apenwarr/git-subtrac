package main

import (
	"fmt"
	//	"github.com/pborman/getopt/v2"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
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
	var out []string
	for _, v := range c.tracs {
		out = append(out, v.String())
	}
	return strings.Join(out, "\n")
}

func (c *Cache) tracByRef(refname string) (*Trac, error) {
	rn := plumbing.NewBranchReferenceName(refname)
	ref, err := c.repo.Reference(rn, true)
	if err != nil {
		return nil, err
	}
	commit, err := c.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	return c.tracCommit(commit)
}

// Mercifully, git's content-addressable storage means there are never
// any cycles when traversing the commit+submodule hierarchy, although the
// same sub-objects may occur many times at different points in the tree.
func (c *Cache) tracCommit(commit *object.Commit) (*Trac, error) {
	trac := c.tracs[commit.Hash]
	if trac != nil {
		return trac, nil
	}
	trac = &Trac{
		name: "<COMMIT>",
		hash: commit.Hash,
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("%.10v.Tree: %v", commit.Hash, err)
	}
	_, err = c.tracTree("<ROOT>", tree)
	if err != nil {
		return nil, fmt.Errorf("%.10v.addTree: %v", commit.Hash, err)
	}
	for _, parent := range commit.ParentHashes {
		pc, err := c.repo.CommitObject(parent)
		if err != nil {
			return nil, fmt.Errorf("%.10v: %v", pc.Hash, err)
		}
		_, err = c.tracCommit(pc)
	}
	c.tracs[commit.Hash] = trac
	return trac, nil
}

func (c *Cache) tracTree(name string, tree *object.Tree) (*Trac, error) {
	trac := c.tracs[tree.Hash]
	if trac != nil {
		return trac, nil
	}
	for _, e := range tree.Entries {
		if e.Mode == filemode.Submodule {
			debugf("submodule: %v/\n", e.Name)
		} else if e.Mode == filemode.Dir {
			t, err := c.repo.TreeObject(e.Hash)
			if err != nil {
				return nil, fmt.Errorf("%.10v.Tree.%.10v: %v",
					tree.Hash, e.Hash, err)
			}
			_, err = c.tracTree(e.Name, t)
			if err != nil {
				return nil, fmt.Errorf("%.10v.addTree: %v",
					t.Hash, err)
			}
		}
	}
	trac = &Trac{
		name: name,
		hash: tree.Hash,
	}
	c.tracs[tree.Hash] = trac
	return trac, nil
}

func main() {
	log.SetFlags(0)
	r, err := git.PlainOpen(".")
	if err != nil {
		fatalf("git.PlainOpen: %v\n", err)
	}
	c := NewCache(r)
	refname := "junk"
	_, err = c.tracByRef(refname)
	if err != nil {
		fatalf("AddByRef: %v: %v\n", refname, err)
	}
	fmt.Printf("%v\n", c)
}
