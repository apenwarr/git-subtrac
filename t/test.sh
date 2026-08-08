#!/bin/sh

mkdir playground
cd playground

# make (bare) parent.git repo
mkdir parent
cd parent
git init
echo parent > README
cp ~/src/swanbase/git-fix-modules.sh .
git add README git-fix-modules.sh
git commit -m 'initial parent commit'
mv .git ../parent.git
cd ..
rm -fr parent
cd parent.git
git config --bool core.bare true
cd ..

# make (bare) dep.git repo
mkdir dep
cd dep
git init
echo dep > README
git add README
git commit -m 'initial dep commit'
mv .git ../dep.git
cd ..
rm -fr dep
cd dep.git
git config --bool core.bare true
cd ..

# repo owner: add dep as a subtrac-enhanced submodule
git clone parent.git parent
cd parent/
git submodule add ../dep
git add .gitmodules dep
git commit -m 'add submodule'
./git-fix-modules.sh
git subtrac --auto-exclude update
git push origin master master.trac
cd ..

# repo contributor: clone
git clone --recurse-submodules parent.git another-parent
cd another-parent
./git-fix-modules.sh
cd dep
git checkout master

# repo contributor: make local change
echo 'local change' >> README
git commit -m 'locally patch dep' README
cd ..
git commit -m 'record change in parent' dep
git subtrac --auto-exclude update
git push origin master master.trac
cd ..

# upstream change
git clone dep.git dep
cd dep
echo 'upstream change' >> README
git commit -m 'upstream change' README
git push origin master
cd ..

# rebase parent/dep after upstream change
cd parent/dep
git fetch
git rebase origin/master
git commit -m 'rebase dep on upstream changes' dep
git push origin master

# push parent/dep changes to upstream
cd parent/dep
git push origin master
