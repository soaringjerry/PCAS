# Foundation Nightly Evaluation

This page gives the nightly evaluation with a real model. The owner decided on 2026-10-12: one US dollar for one night at most, on this host's Codex channel, at night.

## What Runs

`scripts/nightly-eval.sh` runs the synthetic doing suite (`testdata/phase2_5/doing/suite.json`) through `scripts/p25-v2-run.sh`.
The method is `current`: the product's recall route gives the context of each answer. One night has one repetition.
One night runs 60 of the 120 tasks. The other 60 tasks run the next night, so that two nights cover the suite.
Each task makes one answer call and two judge calls. The database is a disposable container that the run script owns and removes.

The run needs a signed-in Codex home that is not the home of the live service.
The script's default path is `/root/PCAS-private/codex-eval`. `PCAS_EVAL_CODEX_HOME` gives another path.
The owner decided on 2026-10-12 to use the host's own Codex home, `/root/.codex`. The scheduled job sets `PCAS_EVAL_CODEX_HOME` to it. The evaluation therefore shares that login and its quota with the other uses of the host's Codex.
A night without a signed-in home makes no call. The script writes `skipped` with the reason and stops with status 2.

## Spending Limit

| Item | Value | Basis |
|---|---|---|
| Limit for one night | 1 USD. `PCAS_NIGHTLY_LIMIT_USD` changes it. | The owner's decision. |
| Price | 1 USD for one million input tokens and 4 USD for one million output tokens. | The list price that the product uses for the same model on an API channel. The Codex channel bills a subscription, so this is a reference price. |
| Tokens | One character counts as one token. | This is above the real count for the suite's text. |
| Answer call | 3,600 input characters and 600 output characters at most. | The dry run of 2026-10-12 with the fake model: the largest answer input was 3,504 characters. 600 characters is the answer limit of the suite. |
| Judge call | 1,800 input characters and 200 output characters. | An estimate. The report does not give the size of a judgment. |

The script computes the cost of the plan before the first call. The plan of 60 tasks costs 0.672 USD at these values.
When a plan is above the limit, the script removes tasks from the end of the plan until it fits, and it writes the count of the tasks that it left out.
After the run the script computes the cost again from the sizes in the report. A result above the limit ends with status 3 and the line `failed`.

## Output

Each night writes `report.json`, `report.md`, and `run.log` to `/var/tmp/pcas-nightly/<date>/`. `PCAS_NIGHTLY_OUTPUT` gives another directory.
The file `nightly.log` in the output directory gets one plan line and one result line for each night: the revision, the tasks, the calls, the estimated cost, and the must and usable rates.
The reports hold metrics and task identifiers only. The suite is synthetic.

## Rehearsal

`PCAS_NIGHTLY_FAKE=1 scripts/nightly-eval.sh` runs the whole script with the fake model. It makes no real call, and its scores mean nothing.
The rehearsal of 2026-10-12 finished with 60 tasks, 180 calls, and an estimated cost of 0.49 USD.

## Schedule

The host's crontab starts the script at 15:30 UTC, which is night in the owner's time zone.
The job updates a worktree of its own to `origin/main` first, so each night evaluates the current main revision.

The script starts the program `codex` that is first in `PATH`. The program must have the app server (`codex app-server`).
This host has two Codex programs: version 0.30.0 in `/usr/local/bin`, which has no app server, and the owner's current program under `/root/.nvm`.
The scheduled job puts the directory of the current program first in `PATH`, and it writes the path and the version of the program to its log.

## First Real Night

The first real night was 2026-10-12, on revision `7cdb96e`.

| Item | Result |
|---|---|
| First start at 15:30 UTC | Failed after nine seconds, before a paid call. The scheduled job found Codex 0.30.0 in `/usr/local/bin`, and the channel check stopped at `initialize` with `transport_error`. |
| Correction | The scheduled job now selects the current Codex program (0.161.0 on that night). The correction is in the job of the host, not in the repository. |
| Second start at 15:31 UTC | Finished in 16 minutes: 60 tasks, 180 calls, model `gpt-6.1-sol`. |
| Estimated cost | 0.5439 USD. The limit is 1 USD. |
| Sizes | The largest answer input was 3,444 characters and the largest answer was 582 characters. Both are below the values of the plan. |
| Must rate | 86.8% for all tasks. By category: direct recall 100%, irrelevant 100%, updated fact 97.5%, indirect use 94%, cross group 76%, outgoing 66%. |
| Usable rate | 65% for all tasks. By category: direct recall 100%, irrelevant 100%, updated fact 80%, indirect use 70%, cross group 40%, outgoing 0%. |
| Other counts | One forbidden hit (cross group). The two judges disagreed on three checks. |

These are the values of one repetition of one half of the suite. They are a first point, not a trend.
The run had no embedding provider, so the recall route used the product's keyword search.
Each night leaves its run directory (`/var/tmp/pcas-v2-run.*`, less than 2 MB) on the host. No rule removes these directories yet.

## Limits of This Evaluation

- The suite has 20 scenarios behind its 120 tasks. A change of a few points between two nights is inside the variation of one repetition.
- The judges are the same model as the answers.
- The evaluation measures the answers that the recall route supports. It does not run the secretary's actions or the background jobs.
