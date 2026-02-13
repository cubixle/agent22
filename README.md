# Agent22
----------------------

Agent22 is a opinionated coding "agent". 

The idea is that Agent22 will pull tickets from JIRA and work on them.

My specific workflow is 

- Add ticket to JIRA as a 'todo'
- When ready for the Agent to work on it move it to 'ready' status
- Agent will pick it up and do the following
- I'll then review those changes and checkout any that need slight modififcations.
- Merge back to main.
- Repeat

The idea for the agent is that it is always running on a machine setup up to complete the tasks.

It works in the following way

    1. Read details
    2. Pull the latest changes
    3. Checkout new branch
    4. Make changes using opencode
    5. Commit & push changes
    6. Create Pull Request in Gitea
    7. Checkout the main branch
    8. Repeat.

Nothing complicated, and the 'admin' actions happen outside of OpenCode to save on tokens. Why waste them when some of 
this stuff is simple cli tools or API queries. 

## Supported Third Parties

- JIRA
- Gitea
- OpenCode

## Config

In each project make sure to have a `.agent22.yml` config file.

Here is an example config file

```yaml
JIRA_EMAIL=
JIRA_JQL="project = example AND status = 'ready'"
JIRA_API_TOKEN=

***REMOVED***
***REMOVED***
GITEA_TOKEN=
***REMOVED***
***REMOVED***
GITEA_BASE_URL=
```
