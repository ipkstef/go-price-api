package models

type Group struct {
	GroupID   int64   `json:"group_id"`
	Name      *string `json:"name"`
	Abbr      *string `json:"abbr"`
	IsCurrent *bool   `json:"is_current"`
}
