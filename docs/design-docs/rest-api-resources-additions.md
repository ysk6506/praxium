---
title: REST API의 자원과 길 (3) — 에이전트 시대의 정의, 밀어 넣기, 매핑을 돕는 길, 목록과 해 보기
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API의 자원과 길 (3)

> **길과 칸은 OpenAPI 문서에서 굳어요. 그때 바뀔 수 있어요.** 길은 `/v1/...`이고 이름은 이 문서가 정한 가정이에요. 자기 관측성의 길(`/v1/ops/...`)은 [프락시움 자신의 관측성](self-observability.md)이, 비밀 값의 길(`/v1/secrets` 등)은 [비밀 값과 암호화](secrets-and-encryption.md)가 정한 이름을 따라요.

[자원 (1)](rest-api-resources-data.md), [자원 (2)](rest-api-resources-graph.md)에 이어, 화면이 할 수 있는 일은 모두 공개된 `/v1`로 된다는 원칙(ADR-0016 10번)에 따라 더한 기능을 자원으로 적어요. 임시 콘솔도 이 길만 불러요([임시 콘솔](temporary-console.md)).

## 1. 목록과 해 보기: 화면이 규칙을 베끼지 않게
| 길 | 방식 | 뜻 | 누가 |
|---|---|---|---|
| `/v1/catalog/...` | GET | 정제 연산(14가지)과 인자의 모양, 해석기와 설정의 모양, 받는 방식, 링크의 뜻과 채널, 값 타입과 순수 함수, 질의 13가지와 인자, 역할, 오류 코드, 사건 코드, 기본값. 모양은 JSON 스키마예요 | 로그인한 누구나 |
| 정의의 모든 항목에 `:validate` | POST | 저장하지 않고 검사만 해요. 칸마다의 오류와 고칠 길의 귀띔을 줘요 | 그 항목을 읽는 권한 |
| 정의의 모든 항목에 `:preview` | POST | 저장하지 않고 미리 봐요 | 같음 |
| 정의의 모든 항목에 `:suggest` | POST | 정해진 규칙으로 초안을 내요(같은 입력이면 같은 답) | 같음 |
| 모든 자원의 `allowed_actions` | 답의 칸 | 지금 이 주체가 할 수 있는 것과 막힌 까닭의 코드 | |

목록은 거르기, 차례, 칸 고르기(`fields`), 함께 싣기(`include`), 커서 넘김을 고르게 받고 전체 수는 청하면 줘요. 여러 건을 한 번에 읽는 길과 여러 건을 한 번에 쓰는 길(브랜치 안의 편집)을 둬요. 이웃과 경로와 영향의 답은 그리기 좋은 꼴(노드와 링크의 목록, 묶음의 요약, 더 펼칠 수 있는 자리의 표시)로도 주고 큰 답은 묶어 요약한 뒤 펼치는 길을 줘요. 작업의 진행, 실행의 단계, 건강과 문제, KG의 변경은 SSE로 내고 끊겼다 이어도 빠진 것이 없게 자리를 줘요. 타입, 속성, 링크, 액션, 코드의 표시 이름과 설명은 언어별로 실을 자리를 둬요. 계약의 검사에 "문서의 기능마다 `/v1`의 길이 있는가"를 더해요.

## 2. 에이전트 시대의 정의(ADR-0014, definitions-at-agent-scale.md)
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| 브랜치와 제안의 칸 | | `authored_by`, `on_behalf_of`, `agent_run_id`, `agent_build`, `trigger_ref`, `evidence`, `intent`, `decision_log_hash`, `risk_grade`, `after` | 쓰는 쪽 |
| 제안의 `:pre-check`(가정) | POST | 큐가 돌릴 검사와 후보 실행을 미리 돌려요. 주체마다 계산 예산이 걸려요 | `editor` |
| 제안의 `:rebase`(가정) | POST | 본줄기에 맞추기. 충돌이 없으면 저절로 돼요 | `editor` |
| `/v1/merge-policies`(가정) | GET, POST | 위험의 등급표와 자동 병합의 정책(정의의 항목). 느슨하게 하는 것은 `admin`의 승인이 들어요 | `admin` |
| `/v1/agent-runs/{agent_run_id}/revert-proposals`(가정) | POST | 에이전트 실행 하나가 만든 병합 전부를 한 묶음의 되돌리기 제안으로 만들어요(미리 보기와 승인을 거쳐요) | `editor` |

승인과 거절은 사람만 해요. MCP의 정의 면은 읽기만이에요.

## 3. 받기를 넓히는 길(ADR-0008, ingestion-extensions.md)
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| `/v1/sources/{source_id}/push`(가정) | POST | 밀어 넣기. 시스템 계정으로 인증해요. 사건 ID로 겹침을 막아요 | 시스템 계정 |
| `/v1/refreshes`(가정) | POST | 객체를 지정해서 다시 받기. 작업을 줘요 | `operator`, 시스템 계정 |
| `/v1/expectations/checks`(가정) | POST | 기대와 견주기. 속성마다 `confirmed`, `not_yet`, `contradicted`, `unverifiable` | `viewer` |
| `/v1/datasets/{dataset_id}/profile`(가정) | GET | 견본 줄과 컬럼 통계(민감 등급과 읽기 권한을 따라요) | `viewer` |
| 공급의 `:suggest` | POST | 매핑과 식별 후보의 제안 | `editor` |
| 객체 타입의 공급 표와 빈 속성의 후보 | GET | 타입 쪽에서 보기 | `viewer` |
| 공급의 `:preview` | POST | 견본 줄 몇 개가 어떤 객체와 링크가 되는지 | `editor` |
| `/v1/parsers/probe`(가정) | POST | 견본 문서를 올려 해석기의 후보와 나무를 봐요 | `editor` |

## 4. 비밀 값과 운영
비밀 값의 길과 감사 기록은 [비밀 값과 암호화](secrets-and-encryption.md)에 있어요. 진단, 운영 행위, 사건 코드, 설정, 올리는 중의 맞음(`/v1/ops/rollout`)은 [프락시움 자신의 관측성](self-observability.md)에 있어요. 떠 있나·받을 수 있나의 거친 답은 로그인 없이 주어요(배포의 점검이 써요).

## 5. 설계 담당에게 묻는 것
- 위 표에서 (가정)이라고 적은 길의 이름.
- 떠 있나·받을 수 있나의 길 이름(설계 문서에 길이 적혀 있지 않아요).
- `allowed_actions`의 칸 모양과 막힌 까닭의 코드 목록.

## 6. 관련
ADR-0016(9, 10번), ADR-0014, ADR-0008. 요구사항: REQ-229(화면을 위한 API의 원칙), REQ-209(임시 콘솔), REQ-220, REQ-225~228.
