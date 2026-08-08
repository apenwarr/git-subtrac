#!/bin/sh
# A more helpful replacement for 'git submodule update --init'.
#
# It leaves the remote 'origin' pointing at the upstream projects, but can
# use local git-subtrac branches as a data source so that most of the time
# it doesn't actually need to fetch from the upstream.
#
# Use this whenever you want to get your submodules back in sync.
#
set -e
cd "$(dirname "$0")"  # move to git worktree root
topdir="$PWD"  # absolute path of git worktree root

# For initial 'git submodule update', the --no-fetch option is ignored,
# and it tries to talk to the origin repo for no good reason. Let's override
# the origin repo URL to fool git into not doing that.
git submodule status | while read -r commit path junk; do
	git submodule init -- "$path"
done
git config --local --get-regexp '^submodule\..*\.url$' | while read k v; do
    git config "$k" .
done

# In each submodule, make sure info/alternates is set up to retrieve
# objects directly from the parent repo (git-subtrac objects), bypassing
# the need to fetch anything. If someone has previously checked out a
# submodule without setting these values, this will fix them up.
for config in .git/modules/*/config; do
	[ -f "$config" ] || continue

	dir=$(dirname "$config")
	echo "$topdir/.git/objects" >"$dir/objects/info/alternates"
done

# Make sure any remaining submodules have been checked out at least once,
# referring to the toplevel repo for all objects.
#
# TODO(apenwarr): --merge is not always the right option.
#  eg. when checking out old revisions, we'd rather just roll the submodule
#  backwards too. But git submodule doesn't have a good way to do that
#  safely, so after a checkout, you can run git-stash-all.sh by hand to
#  rewind the submodules.
git submodule update --init --no-fetch --reference="$PWD" --recursive --merge

# Make sure all submodules are *now* (after initial checkout) using the
# latest URL from .gitmodules for their 'origin' URL.
git submodule --quiet sync --recursive

git submodule status --cached | while read -r commit path junk; do
	# fix superproject conflicts caused by trying to merge submodules,
	# if any. These happen when two different commits try to change the
	# same submodule in incompatible ways. To resolve it, we'll check out
	# the first one and try to git merge the second. (Why git can't just
	# do this by itself is... one of the many problems with submodules.)
	cid2=
	cid3=
	git ls-files --unmerged -- "$path" | while read -r mode hash rev junk; do
		if [ "$rev" = "2" ]; then
			(cd "$path" && git checkout "$hash" -- || true)
			cid2=$hash
		fi
		if [ "$rev" = "3" ]; then
			cid3=$hash
			(cd "$path" && git merge "$hash" -- || true)
			git add -- "$path"
		fi
	done

	commit=${commit#-}
	commit=${commit#+}
	(
		cd "$path"

		main=$(git rev-parse --verify --quiet main || true)
		head=$(git rev-parse --verify HEAD)

		if [ -n "$main" ] &&
		   ! git merge-base main "$commit" >/dev/null; then
			# main and $commit have no common history.
			# It's probably dangerous. Move it aside.
			git branch -f -m main main.probably-broken
		fi
		
		# update --merge can't rewind the branch, only move it
		# forward. Give a warning if we notice this problem.
		if [ "$head" != "$commit" ]; then
			echo "$path:" >&2
			echo "  Couldn't checkout non-destructively." >&2
			echo "  You can try to fix it by hand, or" >&2
			echo "  use git-stash-all.sh if you want to force it." >&2
		fi
	)
done
