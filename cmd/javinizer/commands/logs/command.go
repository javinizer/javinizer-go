package logs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "View structured event logs",
		Long:  `View and search structured event logs for debugging and bug reporting`,
	}

	logsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent events",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runLogsList(cmd, args, configFile)
		},
	}
	logsListCmd.Flags().IntP("limit", "n", 20, "Number of events to show")
	logsListCmd.Flags().StringP("type", "t", "", "Filter by event type (scraper, organize, system)")
	logsListCmd.Flags().StringP("severity", "s", "", "Filter by severity (debug, info, warn, error)")
	logsListCmd.Flags().String("source", "", "Filter by event source")

	logsStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show event statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runLogsStats(cmd, args, configFile)
		},
	}

	logsCmd.AddCommand(logsListCmd, logsStatsCmd)
	return logsCmd
}

func runLogsList(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	eventRepo := database.NewEventRepository(deps.DB)

	limit, _ := cmd.Flags().GetInt("limit")
	eventType, _ := cmd.Flags().GetString("type")
	severity, _ := cmd.Flags().GetString("severity")
	source, _ := cmd.Flags().GetString("source")

	if eventType != "" {
		validTypes := map[string]bool{models.EventCategoryScraper: true, models.EventCategoryOrganize: true, models.EventCategorySystem: true}
		if !validTypes[eventType] {
			return fmt.Errorf("invalid event type %q: must be one of scraper, organize, system", eventType)
		}
	}
	if severity != "" {
		validSeverities := map[string]bool{models.SeverityDebug: true, models.SeverityInfo: true, models.SeverityWarn: true, models.SeverityError: true}
		if !validSeverities[severity] {
			return fmt.Errorf("invalid severity %q: must be one of debug, info, warn, error", severity)
		}
	}

	filter := database.EventFilter{
		EventType: eventType,
		Severity:  severity,
		Source:    source,
	}

	events, err := eventRepo.FindFiltered(filter, limit, 0)
	if err != nil {
		return fmt.Errorf("failed to retrieve events: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No events found")
		return nil
	}

	fmt.Println("=== Event Logs ===")
	fmt.Printf("%-6s %-10s %-8s %-12s %-20s %s\n",
		"ID", "Type", "Sev", "Source", "Time", "Message")
	fmt.Println(strings.Repeat("-", 110))

	for _, event := range events {
		timeStr := event.CreatedAt.Format("2006-01-02 15:04:05")
		msg := event.Message
		if len(msg) > 60 {
			msg = msg[:57] + "..."
		}

		fmt.Printf("%-6d %-10s %-8s %-12s %-20s %s\n",
			event.ID,
			event.EventType,
			event.Severity,
			event.Source,
			timeStr,
			msg,
		)

		if event.Context != "" {
			var ctx map[string]interface{}
			if err := json.Unmarshal([]byte(event.Context), &ctx); err == nil {
				if jobID, ok := ctx["job_id"]; ok && jobID != nil {
					fmt.Printf("       Job: %v\n", jobID)
				}
				if movieID, ok := ctx["movie_id"]; ok && movieID != nil {
					fmt.Printf("       Movie: %v\n", movieID)
				}
				if errVal, ok := ctx["error"]; ok && errVal != nil {
					errStr := fmt.Sprintf("%v", errVal)
					if len(errStr) > 80 {
						errStr = errStr[:77] + "..."
					}
					fmt.Printf("       \x1b[31mError: %s\x1b[0m\n", errStr)
				}
			}
		}
	}

	fmt.Printf("\nShowing %d event(s)\n", len(events))

	return nil
}

func runLogsStats(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	eventRepo := database.NewEventRepository(deps.DB)

	total, err := eventRepo.Count()
	if err != nil {
		return fmt.Errorf("failed to count events: %w", err)
	}

	fmt.Println("=== Event Statistics ===")
	fmt.Printf("\nTotal Events: %d\n", total)

	fmt.Println("\nBy Type:")
	typeCounts := []struct {
		name     string
		category string
		emoji    string
	}{
		{"Scraper", models.EventCategoryScraper, "🌐"},
		{"Organize", models.EventCategoryOrganize, "📦"},
		{"System", models.EventCategorySystem, "⚙️"},
	}
	for _, tc := range typeCounts {
		count, _ := eventRepo.CountByType(tc.category)
		pct := percentage(count, total)
		fmt.Printf("  %s %-10s %d (%.1f%%)\n", tc.emoji, tc.name+":", count, pct)
	}

	fmt.Println("\nBy Severity:")
	sevCounts := []struct {
		name     string
		severity string
		emoji    string
	}{
		{"Error", models.SeverityError, "❌"},
		{"Warn", models.SeverityWarn, "⚠️"},
		{"Info", models.SeverityInfo, "ℹ️"},
		{"Debug", models.SeverityDebug, "🐛"},
	}
	for _, sc := range sevCounts {
		count, _ := eventRepo.CountBySeverity(sc.severity)
		pct := percentage(count, total)
		fmt.Printf("  %s %-8s %d (%.1f%%)\n", sc.emoji, sc.name+":", count, pct)
	}

	sourceCounts, err := eventRepo.CountGroupBySource()
	if err == nil && len(sourceCounts) > 0 {
		fmt.Println("\nBy Source:")
		for src, count := range sourceCounts {
			pct := percentage(count, total)
			fmt.Printf("  %-15s %d (%.1f%%)\n", src+":", count, pct)
		}
	}

	return nil
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
