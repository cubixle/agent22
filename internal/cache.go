package internal

type ResultCache struct {
	cache map[string]JiraIssue
}

func (r *ResultCache) Next() (JiraIssue, bool) {
	for _, issue := range r.cache {
		return issue, true
	}

	return JiraIssue{}, false
}

func (r *ResultCache) Get(key string) (JiraIssue, bool) {
	issue, exists := r.cache[key]
	return issue, exists
}

func (r *ResultCache) Fill(issues []JiraIssue) {
	r.cache = make(map[string]JiraIssue)
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
