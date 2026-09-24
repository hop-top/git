package main

import (
	"fmt"

	"hop.top/git/internal/output"
)

func main() {
	// Initialize output in human mode
	output.SetupLogger(output.ModeHuman, false)

	fmt.Println("=== Git-Hop Enhanced Output Demo ===")

	// 1. Success Card Demo
	fmt.Println("1. Success Card:")
	card := output.SuccessCard("Repository Ready", []output.CardField{
		{Key: "Hub Path", Value: "~/code/org/repo"},
		{Key: "Worktree", Value: "~/code/org/repo/main"},
		{Key: "Branch", Value: "main"},
		{Key: "Services", Value: "api, db (ports: 11500-11502)"},
	})
	fmt.Println(card)
	fmt.Println()

	// 2. Warning Card Demo
	fmt.Println("2. Warning Card:")
	warningCard := output.WarningCard("Confirm Removal", []output.CardField{
		{Key: "Target", Value: "feature-x worktree"},
		{Key: "Path", Value: "~/code/org/repo/feature-x"},
		{Key: "Changes", Value: "3 uncommitted files"},
		{Key: "Services", Value: "2 running (will stop)"},
	})
	fmt.Println(warningCard)
	fmt.Println()

	// 3. Status Table Demo
	fmt.Println("3. Status Table:")
	table := output.NewStatusTable("Branch", "Status", "Env", "Ports")
	table.AddRow("success", "main", "Active", "Running", "11500-02")
	table.AddRow("success", "feature-x", "Clean", "Down", "11503-05")
	table.AddRow("warning", "bugfix-123", "Dirty", "Down", "11506-08")
	table.AddRow("error", "hotfix-456", "Clean", "Error", "11509-11")
	table.Print()
	fmt.Println()

	// 4. Section with Tree Demo
	fmt.Println("4. Section with Tree Structure:")
	section := output.Section("Environment", []string{
		"Status         " + output.Colorize(output.IconRunning, "success"),
		"Started        2h ago",
		"",
		"Services:",
		output.TreeItem(false, "api", output.Colorize(output.IconRunning, "success")+"   11500   health: "+output.Colorize(output.IconSuccess, "success")),
		output.TreeItem(false, "db", output.Colorize(output.IconRunning, "success")+"   11501   health: "+output.Colorize(output.IconSuccess, "success")),
		output.TreeItem(true, "cache", output.Colorize(output.IconStopped, "neutral")+"   11502   health: -"),
	})
	fmt.Println(section)
	fmt.Println()

	// 5. Status Lines Demo
	fmt.Println("5. Status Lines:")
	fmt.Println(output.StatusLine("success", "Worktree created successfully"))
	fmt.Println(output.StatusLine("error", "Failed to start service"))
	fmt.Println(output.StatusLine("warning", "Configuration outdated"))
	fmt.Println(output.StatusLine("info", "Pulling Docker image..."))
	fmt.Println()

	// 6. Aligned List Demo
	fmt.Println("6. Aligned List:")
	list := output.AlignedList([]struct{ Label, Value string }{
		{"Branch", "feature-x"},
		{"Remote", "origin/feature-x (2 commits ahead)"},
		{"Status", output.Colorize(output.IconDirty, "warning") + " (3 files)"},
		{"Path", "~/code/org/repo/feature-x"},
		{"Last Active", "5m ago"},
	})
	fmt.Println(list)
	fmt.Println()

	// 7. Legend Demo
	fmt.Println("7. Legend:")
	legend := output.Legend(map[string]string{
		output.IconActive:  "worktree on disk",
		output.IconClean:   "no local changes",
		output.IconDirty:   "uncommitted changes",
		output.IconRunning: "services up",
		output.IconStopped: "services down",
	})
	fmt.Println(legend)
	fmt.Println()

	// 8. Header and Banner Demo
	fmt.Println("8. Headers:")
	fmt.Println(output.SimpleHeader("Cloning github.com/org/repo"))
	fmt.Println()
	fmt.Println(output.RenderHeader("Repository Ready"))
	fmt.Println()

	// 9. Next Step Hint Demo
	fmt.Println("9. Next Step Hint:")
	fmt.Println(output.NextStepHint("cd org/repo && git hop feature-branch"))

	// 10. Colorized Text Demo
	fmt.Println("10. Colorized Text:")
	fmt.Println(output.Colorize("Success", "success"))
	fmt.Println(output.Colorize("Error", "error"))
	fmt.Println(output.Colorize("Warning", "warning"))
	fmt.Println(output.Colorize("Info", "info"))
	fmt.Println(output.Colorize("Muted", "muted"))
	fmt.Println()

	// 11. Key-Value Rendering
	fmt.Println("11. Key-Value Pairs:")
	fmt.Println(output.RenderKeyValue("Path:", "~/code/org/repo"))
	fmt.Println(output.RenderKeyValue("Branch:", "main"))
	fmt.Println(output.RenderKeyValue("Services:", "api, db"))
	fmt.Println()

	// 12. Icons Demo
	fmt.Println("12. Available Icons:")
	fmt.Println("Status:   ", output.IconSuccess, output.IconError, output.IconWarning, output.IconRunning, output.IconStopped)
	fmt.Println("Tree:     ", output.IconTreeBranch, output.IconTreeLast, output.IconTreeLine)
	fmt.Println("Action:   ", output.IconArrow, output.IconArrowRight, output.IconBulletPoint)
	fmt.Println()

	fmt.Println("=== Demo Complete ===")
}
