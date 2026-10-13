package review

// TrailerInstruction asks the reviewer for the trailer. It is one line on
// purpose: it travels through dash-p as one `--flag=value` token, and a
// newline would split the line that logs the call.
//
// The text is v1's, byte for byte. The review skills and every reviewer
// that already learned the shape read these words, so a change here is a
// change to what comes back.
const TrailerInstruction = "When your reply concludes a PR review task, end it with a fenced code block tagged autoreview containing exactly one JSON object shaped like {\"decision\":\"approved|commented|changes-requested|none\",\"risk\":\"LOW|MEDIUM|HIGH|CRITICAL\",\"findings\":{\"must_fix\":0,\"should_fix\":0,\"polish\":0},\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":2,\"top\":\"MEDIUM\"}],\"blockers\":[{\"severity\":\"HIGH\",\"domain\":\"money\",\"reversible\":false,\"location\":\"src/pay.rs:88\",\"gist\":\"a retried checkout charges the card twice\"}]}. decision is the review that actually landed on the PR: approved = an approving review was submitted, changes-requested = a review that requests changes was submitted, commented = a comment review, or comments alone, landed with no verdict, none = nothing landed on the PR. risk and findings come from the synthesized review. panel lists every launched panelist with its self-reported model, whether it returned a verdict (ok), its finding count, and its top severity. blockers says why the PR is not approved yet, worst first, one entry per must-fix or should-fix finding and none for polish: domain is money, data, security, correctness, ui, perf or docs; reversible is false when the damage cannot be undone once it lands, such as money moved or data lost; gist is one short sentence. Leave blockers empty when you approved. Use null for anything unknown. No prose inside the block."
