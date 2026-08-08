Add subtrac-enhanced submodule to a parent project:

```sh
git clone git@example.com:example/parent.git
cd parent/
git submodule git@example.com:example/dep.git ../dep
git add .gitmodules dep
git commit -m 'add submodule'
cp /path/to/git-subtrac/git-fix-modules.sh .
./git-fix-modules.sh
git subtrac --auto-exclude update
git push origin master master.trac
cd ..
```

Another contributor clones parent repo and makes local change in a submodule:

```sh
git clone --recurse-submodules git@example.com:example/parent.git another-parent
cd another-parent
./git-fix-modules.sh
cd dep
git checkout master

echo 'local change' >> README
git commit -m 'locally patch dep' README
cd ..
git commit -m 'record change in parent' dep
git subtrac --auto-exclude update
git push origin master master.trac
cd ..
```

Bring in changes from upstream and rebase:

```sh
cd parent/dep
git fetch
git rebase origin/master
git commit -m 'rebase dep on upstream changes' dep
git push origin master
cd ../..
```

Share changes with upstream:

```sh
cd parent/dep
git push origin master
cd ../..
```
