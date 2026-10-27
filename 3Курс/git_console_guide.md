# Git Console Guide (for YaeKitsune13)

A compact guide to working with Git from the terminal instead of UI
tools like VS Code.

------------------------------------------------------------------------

# 1. Checking Repository Status

``` bash
git status
```

Shows: - modified files - staged files - untracked files

Example:

    Untracked files:
      file1.py
      file2.py

`U` in VS Code means **Untracked**.

------------------------------------------------------------------------

# 2. Adding Files to the Index (Staging)

Add one file:

``` bash
git add file.py
```

Add several:

``` bash
git add file1.py file2.py
```

Add everything:

``` bash
git add .
```

Check again:

``` bash
git status
```

Now they appear under:

    Changes to be committed

------------------------------------------------------------------------

# 3. Creating a Commit

``` bash
git commit -m "Added new files"
```

A commit saves the current staged changes into the repository history.

------------------------------------------------------------------------

# 4. Sending Changes to GitHub

``` bash
git push
```

Or explicitly:

``` bash
git push origin main
```

Workflow:

    File created
    ↓
    Untracked
    ↓ git add
    Staged
    ↓ git commit
    Local repository
    ↓ git push
    GitHub

------------------------------------------------------------------------

# 5. Viewing Commit History

Basic:

``` bash
git log
```

Better version:

``` bash
git log --oneline --graph
```

Example:

    * a8f21c3 Added login system
    * 9b12f4a Fixed bug
    * 7c21d1a Initial commit

------------------------------------------------------------------------

# 6. Viewing Code Changes

Changes not yet staged:

``` bash
git diff
```

Changes that will go into commit:

``` bash
git diff --cached
```

or

``` bash
git diff --staged
```

------------------------------------------------------------------------

# 7. Working with Branches

Show branches:

``` bash
git branch
```

Create branch:

``` bash
git branch feature
```

Create and switch:

``` bash
git checkout -b feature
```

Modern command:

``` bash
git switch -c feature
```

------------------------------------------------------------------------

# 8. Merging Branches

Switch to main:

``` bash
git switch main
```

Merge:

``` bash
git merge feature
```

Graph example:

    main
      |
      o---o
           \
            o---o feature

------------------------------------------------------------------------

# 9. Temporary Saving Changes (stash)

Save work:

``` bash
git stash
```

Restore:

``` bash
git stash pop
```

------------------------------------------------------------------------

# 10. Resetting Commits

Soft reset (keeps changes):

``` bash
git reset --soft HEAD~1
```

Hard reset (dangerous):

``` bash
git reset --hard HEAD~1
```

------------------------------------------------------------------------

# 11. Restore File Changes

``` bash
git restore file.py
```

Cancels modifications.

------------------------------------------------------------------------

# Advanced Git Commands

These are commonly used by experienced developers.

------------------------------------------------------------------------

# 12. git rebase

Rewrites history to keep it linear.

Before:

    main
    A---B---C
             \
              D---E feature

Command:

``` bash
git switch feature
git rebase main
```

After:

    A---B---C---D---E

⚠ Don't rebase shared branches.

------------------------------------------------------------------------

# 13. git cherry-pick

Take a specific commit from another branch.

``` bash
git cherry-pick HASH
```

Example:

``` bash
git cherry-pick df6619c
```

------------------------------------------------------------------------

# 14. git bisect (Find a Bug Automatically)

Start:

``` bash
git bisect start
```

Mark bad commit:

``` bash
git bisect bad
```

Mark good commit:

``` bash
git bisect good HASH
```

Then test and mark:

    git bisect good
    git bisect bad

Git performs a **binary search** through history.

------------------------------------------------------------------------

# 15. git reflog (Life Saver)

Shows every HEAD movement.

``` bash
git reflog
```

Example:

    df6619c HEAD@{0}: commit
    f361114 HEAD@{1}: commit

Recover lost state:

``` bash
git reset --hard HEAD@{2}
```

------------------------------------------------------------------------

# 16. git blame

Shows who wrote each line.

``` bash
git blame file.py
```

Example:

    df6619c (YaeKitsune13) line 1
    f361114 (YaeKitsune13) line 2

------------------------------------------------------------------------

# 17. Useful Git Log Graph

``` bash
git log --oneline --graph --decorate --all
```

Example:

    * df6619c (HEAD -> main)
    * f361114
    * 69bf990
    | * a514c7a (feature)
    |/
    * 6831810

------------------------------------------------------------------------

# 18. Useful Git Alias

Create a short command:

``` bash
git config --global alias.lg "log --oneline --graph --decorate --all"
```

Then run:

``` bash
git lg
```

------------------------------------------------------------------------

# Recommended Daily Git Workflow

    git pull
    git switch -c feature-task
    (work on code)
    git add .
    git commit -m "task done"
    git push
    git merge feature-task

------------------------------------------------------------------------

End of guide.
