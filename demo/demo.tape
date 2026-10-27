# Record with: vhs demo/demo.tape   (run from the repository root)
# Everything shown comes from demo/setup.sh's generated workspace.
Output demo/demo.gif
Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 580
Set Padding 16
Set TypingSpeed 50ms
Set Framerate 20

Hide
Type "source demo/setup.sh && clear" Enter
Sleep 12s
Show

Type "fell --dry-run" Sleep 300ms Enter
Sleep 3s

# A Claude Code lane holding a commit that exists nowhere else.
Down Sleep 2.5s
Type "r" Sleep 2.5s
Type "f" Sleep 2.5s
Type "y" Sleep 1.5s

# A session that is running right now: fell will not touch it.
Down Sleep 3s

# App-tracked worktrees: strip only, and how the app removes them.
Down Sleep 400ms
Down Sleep 2.5s
Type "r" Sleep 3s
Escape Sleep 500ms

# Select everything and strip the rebuildable artifacts.
Type "a" Sleep 1s
Type "s" Sleep 3s
Type "y" Sleep 2s

Tab Sleep 2s
Type "q" Sleep 3s

# The same scan, for scripts and other tools.
Set TypingSpeed 25ms
Type "fell scan --json | jq -c '.items[] | [.id, .name, .safety.level]'" Sleep 300ms Enter
Sleep 3.5s

Hide
Type "kill $FELL_DEMO_LIVE; cd / && hdiutil detach -quiet /tmp/fell-demo/home; rm -rf /tmp/fell-demo" Enter
Sleep 2s
