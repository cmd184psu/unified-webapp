package store

import "cmd184psu/unified-webapp/internal/issuetracker/models"

// Seed populates demo data if the database is empty.
func (s *Store) Seed() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM teams`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	team, err := s.CreateTeam("ENG", "Engineering", "#5e6ad2")
	if err != nil {
		return err
	}

	alice, _ := s.CreateUser("Alice Chen", "alice@example.com", "#e2a03f")
	bob, _ := s.CreateUser("Bob Diaz", "bob@example.com", "#26a69a")
	s.CreateUser("Carol Smith", "carol@example.com", "#ec4899")

	tagBug, _ := s.CreateTag("bug", "#eb5757")
	tagFeature, _ := s.CreateTag("feature", "#5e6ad2")
	tagBackend, _ := s.CreateTag("backend", "#26a69a")
	tagFrontend, _ := s.CreateTag("frontend", "#bb6bd9")
	tagUrgent, _ := s.CreateTag("urgent", "#f2994a")

	epic, _ := s.CreateEpic(EpicInput{TeamID: &team.ID, Title: ptr("Launch v1"), Description: ptr("Everything needed to ship the first public version."), State: ptr(models.StateInProgress)})
	story, _ := s.CreateStory(StoryInput{TeamID: &team.ID, EpicID: &epic.ID, Title: ptr("Issue tracking core"), Description: ptr("CRUD for issues, tags and relations."), State: ptr(models.StateInProgress)})

	mk := func(title, kind, state string, prio int, assignee *string, tags []string, storyID *string) {
		s.CreateIssue(IssueInput{
			TeamID: &team.ID, Title: ptr(title), Kind: ptr(kind), State: ptr(state),
			Priority: &prio, AssigneeID: assignee, ReporterID: &alice.ID,
			TagIDs: &tags, StoryID: storyID,
			Description: ptr("Auto-generated demo issue for **" + title + "**."),
		})
	}

	mk("Set up SQLite storage layer", "chore", models.StateCompleted, 2, &bob.ID, []string{tagBackend.ID}, &story.ID)
	mk("Design issue list view", "feature", models.StateInProgress, 3, &alice.ID, []string{tagFrontend.ID, tagFeature.ID}, &story.ID)
	mk("Kanban drag and drop", "feature", models.StateTodo, 3, &alice.ID, []string{tagFrontend.ID}, &story.ID)
	mk("Crash when description is empty", "bug", models.StateBlocked, 4, &bob.ID, []string{tagBug.ID, tagUrgent.ID}, &story.ID)
	mk("Linear-compatible GraphQL API", "feature", models.StateInReview, 2, &bob.ID, []string{tagBackend.ID, tagFeature.ID}, nil)
	mk("Add tag color picker", "feature", models.StateBacklog, 1, nil, []string{tagFrontend.ID}, nil)
	mk("Shareable issue links", "feature", models.StateTodo, 2, &alice.ID, []string{tagFeature.ID}, nil)

	// Create a relation between the first two issues.
	issues, _ := s.ListIssues(IssueFilter{TeamID: team.ID})
	if len(issues) >= 2 {
		s.AddRelation(issues[0].ID, issues[1].ID, models.RelBlocks)
	}

	_, _ = tagBug, tagFeature
	return nil
}

func ptr(s string) *string { return &s }
