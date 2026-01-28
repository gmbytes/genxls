package config

type AllConfig struct {
	Items []Item `json:"items"`
	Quests []Quest `json:"quests"`
}

type Item struct {
	Cid int `json:"cid"`
	Count int `json:"count"`
	Data string `json:"data"`
	Dt []int `json:"dt"`
	DtArr [][]int `json:"dtArr"`
}

type Quest struct {
	Cid int `json:"cid"`
	Count int `json:"count"`
	Data string `json:"data"`
	Dt []int `json:"dt"`
	DtArr [][]int `json:"dtArr"`
}
