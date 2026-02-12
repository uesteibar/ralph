package e2e

import (
	"strings"

	mockgithub "github.com/uesteibar/ralph/test/e2e/mocks/github"
	mocklinear "github.com/uesteibar/ralph/test/e2e/mocks/linear"
)

func mocklinearIssue(id, identifier, title string) mocklinear.Issue {
	return mocklinear.Issue{
		ID:          id,
		Identifier:  identifier,
		Title:       title,
		Description: "Test issue description",
		StateID:     mocklinear.StateTodoID,
		StateName:   "Todo",
		StateType:   "unstarted",
	}
}

func mockgithubPR(number int, head, base string) mockgithub.PR {
	return mockgithub.PR{
		Number: number,
		Head:   head,
		Base:   base,
		State:  "open",
	}
}

func stringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
