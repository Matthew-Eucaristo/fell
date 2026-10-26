# Record with: vhs demo/demo.tape   (run from the repository root)
Output demo/demo.gif
Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 640
Set Padding 16
Set TypingSpeed 60ms

Hide
Type "source demo/setup.sh && clear" Enter
Sleep 10s
Show

Type "fell --dry-run" Sleep 400ms Enter
Sleep 2.5s
Down Sleep 600ms
Down Sleep 600ms
Type "s" Sleep 1.5s
Type "y" Sleep 1.5s
Type "r" Sleep 2s
Type "f" Sleep 1.5s
Escape Sleep 800ms
Type "?" Sleep 2.5s
Type "q" Sleep 500ms
Tab Sleep 1.5s
Type "q" Sleep 1.5s

Hide
Type "rm -rf /tmp/fell-demo /private/tmp/fell-demo" Enter
