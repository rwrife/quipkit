package store

// Examples returns the seed snippets written to a fresh snippet dir.
// Kept in a separate file so the list is easy to eyeball and edit.
func Examples() []SeedSnippet {
	return []SeedSnippet{
		{
			Filename: "greeting.md",
			Content: `---
title: Friendly greeting
tags: [hello, casual]
---
Hey! Hope you're doing well.
`,
		},
		{
			Filename: "thanks.md",
			Content: `---
title: Quick thanks
tags: [thanks, casual]
---
Thanks so much — really appreciate it!
`,
		},
		{
			Filename: "follow-up.md",
			Content: `---
title: Gentle follow-up
tags: [followup, work]
---
Just circling back on this — any update when you get a chance?
`,
		},
		{
			Filename: "meeting-decline.md",
			Content: `---
title: Polite meeting decline
tags: [meeting, work, formal]
---
Thanks for the invite. I won't be able to make it, but happy to review
notes async afterwards.
`,
		},
		{
			Filename: "apology.md",
			Content: `---
title: Short apology
tags: [apology, casual]
---
Sorry about that — my mistake. Fixing now.
`,
		},
		{
			Filename: "intro.md",
			Content: `---
title: Personalized intro
tags: [intro, work]
---
Hi {{name}},

Thanks for reaching out on {{date}}. Happy to chat about {{topic:the project}} —
let me know what times work.

Cheers,
{{signature:me}}
`,
		},
	}
}

// SeedSnippet is a single file to be written on first-run seeding.
type SeedSnippet struct {
	Filename string
	Content  string
}
