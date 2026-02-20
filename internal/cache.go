package internal

type ResultCache struct {
	cache map[string]Issue
}

func (r *ResultCache) Next() (Issue, bool) {
	for _, issue := range r.cache {
		return issue, true
	}

	return Issue{}, false
}

func (r *ResultCache) Get(key string) (Issue, bool) {
	issue, exists := r.cache[key]
	return issue, exists
}

func (r *ResultCache) Fill(issues []Issue) {
	r.cache = make(map[string]Issue)
	for _, issue := range issues {
		r.cache[issue.Key] = issue
	}
}

func (r *ResultCache) Delete(key string) {
	delete(r.cache, key)
}

func (r *ResultCache) IsEmpty() bool {
	return len(r.cache) == 0
}
