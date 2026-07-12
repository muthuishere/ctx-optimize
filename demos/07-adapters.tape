Output adapters.gif
Output adapters.mp4
Set FontSize 15
Set Width 1180
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Shell zsh
Set Padding 16
Hide
Type "source /private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/demos/common.sh; rm -rf ~/ctxoptimize/shop-service; cd /private/tmp/claude-501/-Users-muthuishere-muthu-gitworkspace-nexus-workspace-brain/dc4417dd-0040-4de7-82a4-10f10843dc78/scratchpad/demos/shop-service; clear"
Enter
Sleep 1s
Show
Type "# your database schema belongs in the same graph as your code and docs"
Enter
Sleep 1s
Type "ls .ctxoptimize/adapters/"
Enter
Sleep 2s
Type "export DATABASE_URL=postgres://demo:demo@localhost:54329/shop"
Enter
Sleep 1s
Type "ctx-optimize add ."
Enter
Sleep 4s
Type "clear"
Enter
Type "# one question — the answer cites the table, the code, AND the doc"
Enter
Sleep 1s
Type "ctx-optimize ask 'how do refunds work'"
Enter
Sleep 6s
Type "clear"
Enter
Type "# foreign keys are edges: walk them"
Enter
Sleep 1s
Type "ctx-optimize path 'refunds' 'customers'"
Enter
Sleep 5s
