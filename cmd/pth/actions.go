package main

// Action labels reused across CLI dispatcher switches (playtest, user,
// survey, etc.). Pulled out into named constants so goconst stays
// quiet — the strings are CLI verbs the user types, not values
// surfaced anywhere else.
const (
	actionCreate = "create"
	actionEdit   = "edit"
	actionGet    = "get"
	actionList   = "list"
)
