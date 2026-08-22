package models

type Condition struct {
	ConditionID int16  `json:"condition_id"`
	Name        string `json:"name"`
}

type Language struct {
	LanguageID int16  `json:"language_id"`
	Name       string `json:"name"`
}

type Printing struct {
	PrintingID int16  `json:"printing_id"`
	Name       string `json:"name"`
}

type Rarity struct {
	RarityID int16  `json:"rarity_id"`
	Name     string `json:"name"`
}
