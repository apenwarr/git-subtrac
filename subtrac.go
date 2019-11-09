package main

import (
	"fmt"
	//	"github.com/pborman/getopt/v2"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"log"
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
	rn := plumbing.NewBranchReferenceName(refname)
	ref, err := c.repo.Reference(rn, true)
	if err != nil {
		return nil, err
	}
	commit, err := c.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	return c.tracCommit(refname, commit)
}

// Mercifully, git's content-addressable storage means there are never
// any cycles when traversing the commit+submodule hierarchy, although the
// same sub-objects may occur many times at different points in the tree.
func (c *Cache) tracCommit(path string, commit *object.Commit) (*Trac, error) {
	trac := c.tracs[commit.Hash]
	if trac != nil {
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
	}
	c.tracs[commit.Hash] = trac
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
			debugf("submodule: %v/\n", e.Name)
		} else if e.Mode == filemode.Dir {
			t, err := c.repo.TreeObject(e.Hash)
			if err != nil {
				return nil, fmt.Errorf("%v:%.10v: %v",
					path, e.Hash, err)
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
		fatalf("AddByRef: %v\n", err)
	}
	fmt.Printf("%v\n", c)
}
