# git-subtrac: all your git submodules in one place

git-subtrac is a helper tool that makes it easier to keep track of your
git submodule contents. It collects the entire contents of the entire
history of all your submodules (recursively) into a separate git branch,
which can be pushed, pulled, forked, and merged however you want.

## Quick start

git-subtrac is a git extension written in the Go language using the lovely
[go-git](https://github.com/src-d/go-git) library. If you have a Go compiler
set up, you can install the tool like this:

        go install github.com/apenwarr/git-subtrac

This will drop the compiled program into Go's bin directory, often
`$HOME/go/bin`. As long as that's in your `$PATH`, you will then be able to
run commands like `git subtrac <whatever>`.

To collect the submodule history for all your branches into new tracking
branches, just do this (from inside the git repository you want to affect):

        git subtrac update

If your repository is incomplete (ie. you have old submodule links that no
longer exist, because the submodules have since been rebased or something),
this might give an error about missing commits. You can work around it with
the `--auto-exclude` option:

        git subtrac --auto-exclude update

Anyway, `git subtrac update` will generate, for each branch, a new branch
with the `.trac` extension, referencing the complete history of all its
submodule references. The easiest way to use this branch is as follows:

1. Push the tracking branch (eg. `master.trac`) into the same upstream
repository (github or whatever) as the branch (eg. `master`) that it tracks.

2. Edit your `.gitmodules` file to change all the `url = <whatever>` lines
to just `url = .` (a single dot). This tells git to always fetch the
submodule contents from the same repo as your parent module.

End users downloading your project don't need to have git-subtrac installed,
unless they make their own changes to submodules. All the `git submodule`
commands continue to work exactly the same way as before.

## What problem are we solving?

git submodules have been very complicated and hard to use since the earliest
days of git, and have not improved much. The complications stem from some
major design issues:

1. The contents of submodules come from *other* git repositories, which can
  move around or be rebased/deleted/etc without warning, causing your
  own repository to no longer be usable. At the very least, using multiple
  upstream repositories makes it hard to fork a project and make
  wide-ranging changes: you have to fork every sub-repo that you change,
  then update .gitmodules links, tell everyone how to obtain your changes,
  etc. As a result, submodules end up forcing people to centralize on a
  single upstream repository.

2. Submodule links inside your git repository are links to commits, not
  trees. This is incredibly powerful ("this directory is provided by the
  contents of commit X of project P") because it acts like you copied the
  contents of one project into a subdir of another, but you have the complete
  history of the subproject still intact. Unfortunately, this power turns
  git's already-complicated history management (which few people understand)
  into something exponentially more complex. The subproject has (we hope) a
  history that always moves forward (commits always being added on top). And
  your main project has a history that moves forward. But your *link* to the
  subproject can move backward! You can make a new commit to your parent
  project that moves the subproject from commit X to an *earlier* commit X-2
  instead of a *later* commit X+2. As a result, all of git's normal fork,
  merge, revert, stash, etc algorithms are useless for submodules. And sure
  enough, nobody has ever updated them to work well with submodules.

3. Submodules are optional. An early design goal was to make it so that some
  people accessing a project might not have access to all the commits in all
  the subprojects. To make that work, all of git was designed to not mess
  with submodule links when you do regular operations, and to not abort when
  submodule links go missing. So it's super easy to make a mistake like
  forgetting to push you changes to a submodule when you push a change to
  the parent project, thus making it so nobody else can check out your
  parent code anymore.

git-subtrac can't solve all the problems created by these complexities, but
it tries to narrow the set of constraints to make some of them easier. In
particular, we put your submodule contents in the same repository as your
parent project, eliminating all the problems from #1. And we make it easy
to be sure you've pushed all your submodules correctly (just regenerate and
push the trac branch), which greatly reduces #3.

We'll have to look somewhere else for a solution to #2. My earlier tool,
[git-subtree](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.txt)
takes a different approach, avoiding submodule links entirely and merging
submodule content directly into the parent repo. This solves all three of
the problems above, but it makes submitting your changes back upstream more
complex, because you have to split them out again and regenerate the
original submodule history. It also gets confused when you move the linked
subtrees around in your repository, because they just look like files being
moved around, not the history of those files. Still, you might like
git-subtree too. There are some discussions around about the pros and cons
of submodules vs subtrees, such as [this pretty good one from Atlassian](https://www.atlassian.com/git/tutorials/git-subtree).

## Design: How does it actually work?

git-subtrac borrows some tricks from my earlier git-subtree project, but
uses them with a more submodule-centric design.

The main thing you need to know is that, unlike every other kind of git
object, a "submodule" reference from a git tree does *not* cause the
referenced object (a commit) to be included in git object packs, or pushed
along with your branch. In other words, when you add a file (blob) or
subdirectory (tree) to your project, and then commit it to a branch, and
then push that branch, the commit is never sent *without* also including a
copy of the trees and blobs it references. But if your tree links to a
commit - which is all a submodule is, a tree linking to a commit instead of
to another tree or blob - then git push does *not* package up the subcommit
or anything underneath it. It assumes you will push that commit yourself.
And that's where all the problems come from.

The second thing you need to know is that if a commit (say, X) is referenced
as the parent of another commit (say, Y), then when you push commit Y
somewhere, it will always make sure X is there too (either by checking it
already exists, or packaging it up along with Y). This is true recursively,
so if you push the head of your commit history somewhere, then all the
commits it was based on - and all the trees and blobs referred to by those
commits - are pushed along with it. This is normal, of course; you'd be
pretty surprised if that didn't work.

Also relevant is that a commit can have more than one parent commit. When
you make a merge, your "merge commit" has at least two parents. It's not
too commonly used, but a single commit can have as many parents as you want.
You can merge a bunch of branches together in one shot.

So here's what we do: git-subtrac looks through all the trees of all the
commits in your main project, and finds all the submodule links (ie. links
to commit objects). Then it creates a new, parallel history, where for each
commit, the "parent commits" of that commit include not just the "real"
parent(s), but also the commits referred to inside the trees of that commit.
In other words, if you have a commit Y that is based on X, and Y's
filesystem contains submodule links to A, B, and C, then we produce a new
commit Y+, which has parents X+, A, B, and C. X+ is generated the same way,
by appending the submodule links referred to by X.

This way, when you push Y+ to a git server somewhere, it will include A, B,
and C... and all their trees, files, and parents, exactly like you might
have expected in the first place.

### Features of the subtrac history branch

There are a few subtleties here that make the results extra nice. First of
all, the way we generate the synthetic commits is completely stable: anyone
seeing a commit Y (and a copy of its entire history, including all its
submodule links and their trees, blobs, and histories) can reproduce exactly
the same Y+, including the modification dates, parents, and even the commit
hashes. 

But it goes even further. Someone who doesn't have commit Y, but does have
commit X, can generate exactly the same X+ as the person who generates Y+
from Y. (Remember that Y+ depends on X+.) If you have a subset of the
history of Y, you can generate a perfect subset of the history of Y+.

This is really important: it means that two different people, given the same
input, can always regenerate the same git-subtrac output. As long as you
have all the objects, there's no danger in deleting and regenerating the
subtrac branch from scratch.

Secondly, git-subtrac trims redundancy out of the `.trac` branch as far as
possible. It only generates a new Y+ for commit Y (based on X) if the set of
submodule links is different from X. If the submodules haven't changed, then
commit Y+ *is* X+ (not a new commit). This minimizes the changes to your
`.trac` branch across new commits, rebases, etc.

As a result, we get the following features for free:

- If two people differently extend commit X, for example by producing commit
  Y and commit Z on top of X, then their subtrac trees Y+ and Z+ will both
  have X+ as a parent. When you then merge Y and Z together (into, say,
  commit YZ), the resulting YZ+ commit will also look like a git merge of Y+
  and Z+. That merge could be produced by git just as easily as by
  git-subtrac. The main outcome is that you should always be able to push a
  newly generated `.trac` branch to upstream without using a "force push",
  because it should always look like a "fast forward" of the previous
  branch, even though it's actually been regenerated from scratch every time.

- Imagine that commit X of our project depends on commit N of a submodule.
  For some reason commit N doesn't work out very well, and in commit Y we
  rewind the submodule to commit M (one commit before N) while we wait for
  the submodule people to fix it. In our fork of the submodule, we then
  realize we want to apply a cherry pick of some other patch Q, so the
  'master' branch of our forked submodule looks like M+Q. If we push that
  new master branch to our real submodule repo, it would create a surprising
  problem: our subproject's commit Y works fine, because it links to M+Q.
  But if someone then wants to rewind to an earlier version of our
  superproject (eg. using `git bisect`) and try commit X again, it won't
  work! The submodule repo no longer contains N at all, because we rewound
  the submodule's master branch.

  In this case, git-subtrac helps a lot. Since X+ includes N, and Y+
  includes M+Q, *both* sets of submodule links are included in the `.trac`
  branch. We end up successfully tracking "the history of history" of the
  submodules.

- Similarly, a common use of submodules is to maintain patch queues for
  sending upstream. That is, we pull in some version of a submodule project
  (a library), and start working on improving it, testing our patches with
  our superproject (an app) until we're sure they really work. When we're
  feeling confident, we want to rebase the subproject to the latest version
  and apply our patches on top before sending them to the subproject's
  maintainer. In a traditional submodule setup, we'd have to be very careful
  not to lose the *old* history when we rebase the master branch of the
  submodule. With git-subtrac, this all works exactly like you want it to:
  both the old and the new patch histories are available when you need them.

- Forking a superproject (eg. on github) now carries all its submodule
  history along with it, making pull requests very easy. (You can submit a
  pull request for the .trac branch if you want to share your submodule
  patches that way, I guess.)

# Questions, comments?

Please try it out! git-subtrac is fairly new, so I apologize for any bugs
you might run into.

You can email me at apenwarr@gmail.com. I don't have as much free time as I
used to, but I'll try to respond. If you fix a bug, please send pull
requests on github.
