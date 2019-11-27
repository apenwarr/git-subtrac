package main

import (
	"bufio"
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

// A Trac represents a commit or tree somewhere in the project's hierarchy,
// including submodules. If one of its children contains submodules,
// subHeads and tracCommit will be populated.
type Trac struct {
	name       string         // a human-readable path to this object
	hash       plumbing.Hash  // the git hash of this object
	parents    []*Trac        // parent commits (if this is a commit)
	subHeads   []*Trac        // submodule commits contained by this
	tracCommit *object.Commit // synthetic commit with parents+subHeads
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
	repoDir     string                  // toplevel repo dir
	repo        *git.Repository         // open copy of the toplevel repo
	autoexclude bool                    // --auto-exclude enabled
	excludes    map[plumbing.Hash]bool  // specifically excluded objects
	tracs       map[plumbing.Hash]*Trac // object lookup cache
	srPaths     []string                // subrepo paths cache
	srRepos     []*git.Repository       // subrepo object cache
}

func NewCache(rdir string, r *git.Repository, excludes []string,
	autoexclude bool,
	debugf, infof func(fmt string, args ...interface{})) (*Cache, error) {
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

	wt, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("git worktree: %v", err)
	}
	f, err := wt.Filesystem.Open(".trac-excludes")
	if err == nil { // file might not exist, but that's ok
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			// trim comments
			pos := strings.Index(line, "#")
			if pos >= 0 {
				line = line[:pos]
			}
			// trim whitespace
			line = strings.TrimSpace(line)

			if line != "" {
				hash := plumbing.NewHash(line)
				c.exclude(hash)
			}
		}
	}

	return &c, nil
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

// Add one commit to the exclusion list.
func (c *Cache) exclude(hash plumbing.Hash) {
	if !c.excludes[hash] {
		c.excludes[hash] = true
	}
}

// Load all branches into the cache, and update a .trac ref for each one.
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
		} else if commit == nil {
			c.infof("Warning: no submodule commits found for %v; skipping.\n", name)
		} else {
			branches = append(branches, b)
			commits = append(commits, commit)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(branches) != len(commits) {
		return fmt.Errorf("weird: branches=%d commits=%d", len(branches), len(commits))
	}

	for i := range branches {
		newname := string(branches[i].Name()) + ".trac"
		cc := commits[i]
		hash := cc.Hash
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

// Generate a synthetic commit for the given ref.
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

// Starting at the given commit, load all its recursive parents and
// submodule references into the cache, returning the cache entry.
//
// This doesn't update any references in the repo itself, it just returns a
// new object representing the commit, including its synthetic trac commit.
//
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
	var newHeads []*Trac
	var tracs []*object.Commit

	for _, p := range trac.parents {
		if p.tracCommit != nil {
			if !seenTracs[p.tracCommit.Hash] {
				seenTracs[p.tracCommit.Hash] = true
				tracs = append(tracs, p.tracCommit)
			}
		}
		for _, h := range p.subHeads {
			// parent tracCommit already includes this subHead,
			// so we don't need to include it again.
			seenHeads[h.hash] = true
		}
	}
	for _, h := range trac.subHeads {
		if !seenHeads[h.hash] {
			seenHeads[h.hash] = true
			newHeads = append(newHeads, h)
			if h.tracCommit != nil {
				if !seenTracs[h.tracCommit.Hash] {
					seenTracs[h.tracCommit.Hash] = true
					tracs = append(tracs, h.tracCommit)
				}
			}
		}
	}

	if len(newHeads) == 0 && len(tracs) <= 1 {
		if len(tracs) == 1 {
			// Nothing added since our parent, no new commit needed.
			trac.tracCommit = tracs[0]
		}
	} else {
		// Generate a new commit that includes our parent(s) and our
		// new submodules.
		trac.tracCommit, err = c.newTracCommit(commit, tracs, newHeads)
		if err != nil {
			return nil, err
		}
	}

	c.add(trac)
	return trac, nil
}

// True if a and b are equal.
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

// Given a list of parents and submodules for a given real git commit,
// produce a synthetic trac commit that includes all parents and submodules,
// but not the commit itself.
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

	msg := fmt.Sprintf("[git-subtrac for %v]", commit.Hash)
	tc := &object.Commit{
		Author:       sig,
		Committer:    sig,
		TreeHash:     emptyTreeHash,
		ParentHashes: parents,
		Message:      msg,
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

// Update a "commit path" which represents how we get to a given commit
// from a starting point. So if the starting point is "master^2~25, and sub is
// 1, the result is master^2~26. If sub is 3, the result is master^2~25^3, and
// so on.
//
// These paths look weird but are valid git syntax, and are somewhat human-
// friendly once you get used to them.
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

// Recursively open all submodule repositories, starting at c.repo, and
// return a list of them.
func (c *Cache) allSubrepos() (paths []string, repos []*git.Repository, err error) {
	if c.srPaths != nil && c.srRepos != nil {
		return c.srPaths, c.srRepos, nil
	}

	var recurse func(string, *git.Repository) error
	recurse = func(path string, r *git.Repository) error {
		wt, err := r.Worktree()
		if err != nil {
			return fmt.Errorf("git worktree(%s): %v", path, err)
		}
		subs, err := wt.Submodules()
		if err != nil {
			return fmt.Errorf("git submodules(%s): %v", path, subs)
		}
		for _, sub := range subs {
			subpath := path
			if subpath != "" {
				subpath += "/modules/"
			}
			subpath += sub.Config().Name

			ss, err := sub.Status()
			if err != nil {
				return fmt.Errorf("git status(%v): %v", subpath, err)
			}
			empty := plumbing.Hash{}
			if ss.Current == empty {
				// not currently initialized
				c.infof("git submodule(%s): not initialized; skipping\n", subpath)
				continue
			}

			subr, err := sub.Repository()
			if err != nil {
				return fmt.Errorf("git repo(%v): %v", subpath, err)
			}
			paths = append(paths, subpath)
			repos = append(repos, subr)
			err = recurse(subpath, subr)
			if err != nil {
				return err
			}
		}
		return nil
	}

	err = recurse("", c.repo)
	if err != nil {
		return nil, nil, err
	}

	// Cache entries for next time
	c.srPaths = paths
	c.srRepos = repos
	return paths, repos, nil
}

type NotPresentError struct{}

var NotPresent = &NotPresentError{}

// Try to find a given commit object in all submodule repositories. If it
// exists, 'git fetch' it into the main repository so we can refer to it
// as a parent of our synthetic commits.
func (c *Cache) tryFetchFromSubmodules(path string, hash plumbing.Hash) (*NotPresentError, error) {
	c.infof("Searching submodules for: %v\n", path)
	paths, repos, err := c.allSubrepos()
	if err != nil {
		return nil, err
	}
	for i := range repos {
		subpath := paths[i]
		subr := repos[i]
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
			return nil, fmt.Errorf("submodule %v: create %v: %v", subpath, ref, err)
		}
		// TODO(apenwarr): go-git should provide this path?
		//  Maybe it does, but I can't figure out where.
		remotename := fmt.Sprintf("%v/.git/modules/%v", c.repoDir, subpath)
		absremotename, err := filepath.Abs(remotename)
		if err != nil {
			return nil, fmt.Errorf("AbsPath(%v): %v", remotename, err)
		}
		remote, err := c.repo.CreateRemoteAnonymous(&config.RemoteConfig{
			Name: "anonymous",
			URLs: []string{absremotename},
		})
		if err != nil {
			return nil, fmt.Errorf("submodule %v: CreateRemote: %v", absremotename, err)
		}
		err = remote.Fetch(&git.FetchOptions{
			RemoteName: "anonymous",
			RefSpecs: []config.RefSpec{
				config.RefSpec(brrefname + ":TRAC_FETCH_HEAD"),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("submodule %v: fetch: %v", absremotename, err)
		}
		// Fetch worked!
		err = subr.Storer.RemoveReference(brrefname)
		if err != nil {
			return nil, fmt.Errorf("submodule %v: remove %v: %v", subpath, ref, err)
		}
		return nil, nil
	}
	return NotPresent, fmt.Errorf("%v: %v not found.", path, hash)
}

// Starting from a given git tree object, recursively add all its subtree
// and submodules into the cache, returning the cache object representing
// this tree.
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
					npErr, err := c.tryFetchFromSubmodules(subpath, e.Hash)
					if npErr != nil && c.autoexclude {
						c.infof("Excluding %v\n", e.Hash)
						c.exclude(e.Hash)
						continue
					}
					if err != nil {
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

// Add a given entry into the cache.
func (c *Cache) add(trac *Trac) {
	c.debugf("  add %.10v %v\n", trac.hash, trac.name)
	c.tracs[trac.hash] = trac
}
