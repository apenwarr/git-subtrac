package main

import (
	"fmt"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Trac struct {
	name       string
	hash       plumbing.Hash
	parents    []*Trac
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
	debugf      func(fmt string, args ...interface{})
	infof       func(fmt string, args ...interface{})
	repoDir     string
	repo        *git.Repository
	autoexclude bool
	excludes    map[plumbing.Hash]bool
	tracs       map[plumbing.Hash]*Trac
}

func NewCache(rdir string, r *git.Repository, excludes []string,
	autoexclude bool,
	debugf, infof func(fmt string, args ...interface{})) *Cache {
	c := Cache{
		debugf:      debugf,
		infof:       infof,
		repoDir:     rdir,
		repo:        r,
		autoexclude: autoexclude,
		excludes:    make(map[plumbing.Hash]bool),
		tracs:       make(map[plumbing.Hash]*Trac),
	}
	for _, x := range excludes {
		hash := plumbing.NewHash(x)
		c.exclude(hash)
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

func (c *Cache) exclude(hash plumbing.Hash) {
	if !c.excludes[hash] {
		c.excludes[hash] = true
		c.infof("Excluding %v\n", hash)
	}
}

func (c *Cache) UpdateBranchRefs() error {
	branchIter, err := c.repo.Branches()
	if err != nil {
		return fmt.Errorf("GetBranches: %v", err)
	}

	var branches []*plumbing.Reference
	var commits []*object.Commit
	err = branchIter.ForEach(func(b *plumbing.Reference) error {
		name := string(b.Name())
		if strings.HasSuffix(name, ".trac") {
			return nil
		}
		c.infof("Scanning branch: %v\n", name)
		commit, err := c.TracByRef(name)
		if err != nil {
			return err
		} else {
			branches = append(branches, b)
			commits = append(commits, commit)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for i := range branches {
		newname := string(branches[i].Name()) + ".trac"
		hash := commits[i].Hash
		c.infof("Updating %.10v -> %v\n", hash, newname)

		refname := plumbing.ReferenceName(newname)
		ref := plumbing.NewHashReference(refname, hash)
		err = c.repo.Storer.SetReference(ref)
		if err != nil {
			return fmt.Errorf("update %v: %v", refname, err)
		}
	}

	return nil
}

func (c *Cache) TracByRef(refname string) (*object.Commit, error) {
	h, err := c.repo.ResolveRevision(plumbing.Revision(refname))
	if err != nil {
		return nil, fmt.Errorf("%v: %v", refname, err)
	}
	commit, err := c.repo.CommitObject(*h)
	if err != nil {
		return nil, fmt.Errorf("%v: %v", refname, err)
	}
	tc, err := c.tracCommit(refname, commit)
	if err != nil || tc == nil {
		return nil, err
	}
	return tc.tracCommit, nil
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
	ttrac, err := c.tracTree(path+"/", tree)
	if err != nil {
		return nil, err
	}

	// The list of submodules owned by the tree is the same as the list
	// owned by the commit.
	trac.subHeads = ttrac.subHeads

	for i, parent := range commit.ParentHashes {
		pc, err := c.repo.CommitObject(parent)
		if err != nil {
			return nil, fmt.Errorf("%v:%.10v: %v", path, pc.Hash, err)
		}
		np := commitPath(path, i+1)
		ptrac, err := c.tracCommit(np, pc)
		if err != nil {
			return nil, err
		}
		trac.parents = append(trac.parents, ptrac)
	}

	seenHeads := make(map[plumbing.Hash]bool)
	seenTracs := make(map[plumbing.Hash]bool)
	var heads []*Trac
	var tracs []*object.Commit

	for _, h := range trac.subHeads {
		if !seenHeads[h.hash] {
			seenHeads[h.hash] = true
			heads = append(heads, h)
		}
	}
	for _, p := range trac.parents {
		if p.tracCommit != nil {
			if !seenTracs[p.tracCommit.Hash] {
				seenTracs[p.tracCommit.Hash] = true
				tracs = append(tracs, p.tracCommit)
			}
		}
	}

	if len(trac.parents) == 1 && equalSubs(trac.subHeads, trac.parents[0].subHeads) {
		// Nothing has changed since our parent, no new commit needed.
		trac.tracCommit = trac.parents[0].tracCommit
	} else {
		// Generate a new commit that includes our parent(s) and all
		// our submodules.
		trac.tracCommit, err = c.newTracCommit(commit, tracs, heads)
		if err != nil {
			return nil, err
		}
	}

	c.add(trac)
	return trac, nil
}

func equalSubs(a, b []*Trac) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].hash != b[i].hash {
			return false
		}
	}
	return true
}

func (c *Cache) newTracCommit(commit *object.Commit, tracs []*object.Commit, heads []*Trac) (*object.Commit, error) {
	var parents []plumbing.Hash

	// Inherit from our parent tracCommits
	for _, c := range tracs {
		parents = append(parents, c.Hash)
	}
	// *Also* inherit from the actual submodule heads included in the
	// current commit
	for _, h := range heads {
		parents = append(parents, h.hash)
	}

	sig := object.Signature{
		Name:  "git-subtrac",
		Email: "git-subtrac@",
		When:  commit.Committer.When,
	}
	emptyTree := object.Tree{}
	nec := c.repo.Storer.NewEncodedObject()
	err := emptyTree.Encode(nec)
	if err != nil {
		return nil, fmt.Errorf("emptyTree.Encode: %v", err)
	}
	emptyTreeHash, err := c.repo.Storer.SetEncodedObject(nec)
	if err != nil {
		return nil, fmt.Errorf("emptyTree.Store: %v", err)
	}

	tc := &object.Commit{
		Author:       sig,
		Committer:    sig,
		TreeHash:     emptyTreeHash,
		ParentHashes: parents,
		Message:      "[git-subtrac merge]",
	}
	nec = c.repo.Storer.NewEncodedObject()
	err = tc.Encode(nec)
	if err != nil {
		return nil, fmt.Errorf("commit.Encode: %v", err)
	}
	tch, err := c.repo.Storer.SetEncodedObject(nec)
	if err != nil {
		return nil, fmt.Errorf("commit.Store: %v", err)
	}
	tc.Hash = tch
	return tc, nil
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

func (c *Cache) tryFetchFromSubmodules(path string, hash plumbing.Hash) error {
	c.infof("Searching submodules for: %v\n", path)
	wt, err := c.repo.Worktree()
	if err != nil {
		return fmt.Errorf("git worktree: %v", err)
	}
	subs, err := wt.Submodules()
	if err != nil {
		return fmt.Errorf("git submodules: %v", subs)
	}
	for _, sub := range subs {
		subpath := sub.Config().Path
		subr, err := sub.Repository()
		if err != nil {
			return fmt.Errorf("submodule %v: %v", subpath, err)
		}
		_, err = subr.CommitObject(hash)
		if err != nil {
			c.infof("  ...not in %v\n", subpath)
			continue
		}
		c.infof("  ...found! in %v\n", subpath)
		brname := fmt.Sprintf("subtrac-tmp-%v", hash)
		brrefname := plumbing.NewBranchReferenceName(brname)
		ref := plumbing.NewHashReference(brrefname, hash)
		err = subr.Storer.SetReference(ref)
		defer subr.Storer.RemoveReference(brrefname)
		if err != nil {
			return fmt.Errorf("submodule %v: create %v: %v", subpath, ref, err)
		}
		remotename := fmt.Sprintf("%v/.git/modules/%v",
			c.repoDir, sub.Config().Name)
		absremotename, err := filepath.Abs(remotename)
		if err != nil {
			return fmt.Errorf("AbsPath(%v): %v", remotename, err)
		}
		remote, err := c.repo.CreateRemoteAnonymous(&config.RemoteConfig{
			Name: "anonymous",
			URLs: []string{absremotename},
		})
		if err != nil {
			return fmt.Errorf("submodule %v: CreateRemote: %v", absremotename, err)
		}
		err = remote.Fetch(&git.FetchOptions{
			RemoteName: "anonymous",
			RefSpecs: []config.RefSpec{
				config.RefSpec(brrefname + ":TRAC_FETCH_HEAD"),
			},
		})
		if err != nil {
			return fmt.Errorf("submodule %v: fetch: %v", absremotename, err)
		}
		// Fetch worked!
		err = subr.Storer.RemoveReference(brrefname)
		if err != nil {
			return fmt.Errorf("submodule %v: remove %v: %v", subpath, ref, err)
		}
		return nil
	}
	return fmt.Errorf("%v: %v not found.", path, hash)
}

func (c *Cache) tracTree(path string, tree *object.Tree) (*Trac, error) {
	trac := c.tracs[tree.Hash]
	if trac != nil {
		return trac, nil
	}
	trac = &Trac{
		name: path,
		hash: tree.Hash,
	}
	for _, e := range tree.Entries {
		if e.Mode == filemode.Submodule {
			if c.excludes[e.Hash] {
				// Pretend it doesn't exist; don't link to it.
				continue
			}
			subtrac := c.tracs[e.Hash]
			if subtrac == nil {
				subpath := fmt.Sprintf("%s%s@%.10v", path, e.Name, e.Hash)
				sc, err := c.repo.CommitObject(e.Hash)
				if err != nil {
					err = c.tryFetchFromSubmodules(subpath, e.Hash)
					if err != nil {
						if c.autoexclude {
							c.exclude(e.Hash)
							continue
						}
						return nil, fmt.Errorf("%v (fetch it manually? or try --exclude)", err)
					}
				}
				sc, err = c.repo.CommitObject(e.Hash)
				if err != nil {
					return nil, fmt.Errorf("%v: %v",
						subpath, err)
				}
				subtrac, err = c.tracCommit(subpath, sc)
				if err != nil {
					return nil, err
				}
			}
			// Add exactly one submodule.
			// subtrac.tracCommit includes any submodules which
			// that submodule itself depends on.
			trac.subHeads = append(trac.subHeads, subtrac)
		} else if e.Mode == filemode.Dir {
			t, err := c.repo.TreeObject(e.Hash)
			if err != nil {
				return nil, fmt.Errorf("%v:%.10v: %v",
					path+e.Name, e.Hash, err)
			}
			subtrac, err := c.tracTree(path+e.Name+"/", t)
			if err != nil {
				return nil, err
			}
			// Collect the list of submodules all the way down the tree.
			trac.subHeads = append(trac.subHeads, subtrac.subHeads...)
		}
	}
	c.add(trac)
	return trac, nil
}

func (c *Cache) add(trac *Trac) {
	c.debugf("  add %.10v %v\n", trac.hash, trac.name)
	c.tracs[trac.hash] = trac
}
