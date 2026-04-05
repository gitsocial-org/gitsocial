# Usage: DEMO_REPO=/path/to/repo vhs demo.tape

Output demo.gif
Output demo.mp4

Set Shell zsh
Set FontSize 22
Set Width 1920
Set Height 1080
Set Padding 20
Set Theme "Builtin Dark"
Set LoopOffset 100%

# Hidden phase
Hide
Sleep 1s
Type "cd ${DEMO_REPO} && gitsocial tui"
Enter
Sleep 3s
Show

# Timeline
Sleep 2s

# Search
Type "/"
Sleep 1s
Type "single"
Sleep 1s
Enter
Sleep 1s
Down
Sleep 1s
Enter
Sleep 2s

# Pull Requests
Type "P"
Sleep 2s
Down
Down
Sleep 500ms
Enter
Sleep 2s
Type "d"
Sleep 2s
Type "E"
Sleep 2s
Type "v"
Sleep 1s
Type "v"
Sleep 1s
PageDown
Sleep 500ms
PageDown
Sleep 500ms
PageDown
Sleep 500ms
PageDown
Sleep 500ms
PageDown
Sleep 500ms
Escape

# Issue Board
Type "B"
Sleep 2s
Down
Enter
Sleep 4s

# Releases
Type "R"
Sleep 2s
Enter
Sleep 2s
