---
title: REST API의 자원과 길 (3) — 목록과 해 보기, 에이전트 시대의 정의, 밀어 넣기, 비밀 값과 운영
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API의 자원과 길 (3)

> **길과 칸은 OpenAPI 문서에서 굳어요.** 길은 `/v1/...`이고 행위는 `:행위` 표기 하나예요(rest-api.md 3-8). 자기 관측성의 길은 [프락시움 자신의 관측성](self-observability.md)이, 비밀 값의 길은 [비밀 값과 암호화](secrets-and-encryption.md)가 정한 이름을 따르되 행위 표기는 이 문서와 맞춰요.

[자원 (1)](rest-api-resources-data.md), [자원 (2)](rest-api-resources-graph.md)에 이어, 화면이 할 수 있는 일은 모두 공개된 `/v1`로 된다는 원칙(ADR-0016 10번)에 따라 더한 기능을 자원으로 적어요. 임시 콘솔도 이 길만 불러요([임시 콘솔](temporary-console.md)). 기능마다 길이 있는지는 [기능마다의 길](rest-api-feature-routes.md)의 표가 봐요.

## 1. 목록과 해 보기: 화면이 규칙을 베끼지 않게
### 1-1. 목록(`/v1/catalog/...`)
모양은 JSON 스키마예요. 로그인한 누구나 읽어요. `GET /v1/catalog`가 아래의 목록을 줘요. 이미 다른 자원인 것은 길을 따로 만들지 않고 그 길을 가리켜요.

| 목록 | 길 | 담는 것 |
|---|---|---|
| 정제 연산 | `/v1/catalog/refinement-ops` | 14가지와 연산마다 인자의 모양 |
| 해석기 | `/v1/catalog/parsers` | 해석기와 설정의 모양 |
| 받는 방식 | `/v1/catalog/receive-modes` | 데이터셋의 `receive_mode`로 고를 수 있는 넷과 방식마다의 설정의 모양 |
| 링크의 뜻 | `/v1/catalog/link-meanings` | 열하나와 뜻마다의 규칙 |
| 채널 | `/v1/catalog/channels` | 상태 채널 여섯 |
| 값 타입 | `/v1/value-types` | 값 타입(앞 문서 2절. 같은 목록이라 다른 길을 두지 않아요) |
| 순수 함수 | `/v1/catalog/pure-functions` | 값을 다듬는 닫힌 함수와 인자 |
| 닫힌 질의 | `/v1/catalog/queries` | 13가지와 인자의 모양 |
| 역할 | `/v1/roles` | 역할 여덟(그래프 문서 4절. 같은 목록) |
| 오류 코드 | `/v1/catalog/error-codes` | `code`와 뜻(`/v1/problems/{code}`가 한 건의 설명) |
| 사건 코드 | `/v1/catalog/event-codes`, `/v1/catalog/event-codes/{code}` | 뜻, 심각도, 살펴볼 것, 할 수 있는 운영 행위. `catalog_version`이 있어요 |
| 막힌 까닭의 코드 | `/v1/catalog/reason-codes` | `allowed_actions`의 `reason_code` |
| 기본값 | `/v1/catalog/defaults` | 기본값(설정)의 목록 |

### 1-2. 저장하지 않고 해 보기
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| `/v1/definition-items:validate` | POST | 항목(`kind`와 `body`, 없던 것이면 `item_id`는 비워요)을 저장하지 않고 검사해요. 칸마다의 오류와 고칠 길의 귀띔을 줘요. `branch_id`를 받아요 | 그 항목을 읽는 권한 |
| `/v1/definition-items:preview` | POST | 저장하지 않고 미리 봐요. `kind: feed`이면 견본 줄 몇 개가 어떤 객체와 링크가 되는지(식별이 기존 객체에 붙나 새로 만드나, 타입이 안 맞아 떨어지는 값) | 같음. 남의 소스를 읽는 정의이면 읽는 소스의 범위도 있어야 해요 |
| `/v1/definition-items:suggest` | POST | 정해진 규칙으로 초안을 내요(같은 입력이면 같은 답). `kind: feed`이면 컬럼과 속성의 짝과 식별 후보를 까닭과 함께. 저절로 적용하지 않아요 | `editor` |
| 모든 자원의 `meta.allowed_actions` | 답의 칸 | 지금 이 주체가 할 수 있는 것과 막힌 까닭의 코드(rest-api.md 4절) | |

### 1-3. 고르게 받는 목록, 여러 건, 그리기 좋은 꼴
- 목록은 거르기, 차례, 칸 고르기(`fields`), 함께 싣기(`include`), 커서 넘김을 고르게 받고 전체 수는 청하면 줘요.
- 여러 건을 한 번에 읽기: `POST /v1/kgs/{kg}/objects:batch-get`, `POST /v1/definition-items:batch-get`(안전함). 한 번에 쓰기: `POST /v1/branches/{branch_id}/items:batch`.
- 이웃과 경로와 영향의 답은 그리기 좋은 꼴(노드와 링크의 목록, 묶음의 요약, 더 펼칠 수 있는 자리의 표시)로도 줘요(`shape=graph`). 큰 답은 묶어 요약한 뒤 펼치는 길을 줘요.
- 작업의 진행, 실행의 단계, 건강과 문제, KG의 변경은 사건의 길로 내요(rest-api.md 3-10).
- 타입, 속성, 링크, 액션, 코드의 표시 이름과 설명은 언어별로 실을 자리(`display_names`)가 있어요.

## 2. 에이전트 시대의 정의(ADR-0014, definitions-at-agent-scale.md)
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| 브랜치와 제안의 칸 | | `authored_by`, `on_behalf_of`, `agent_run_id`, `agent_build`, `trigger_ref`, `evidence`, `intent`, `decision_log_hash`, `risk_grade`, `after` | 쓰는 쪽 |
| `/v1/proposals/{id}/checks`, `/v1/proposals/{id}/candidate-runs` | POST | 큐가 돌릴 검사와 후보 실행을 미리 돌려요(초안 제안에서도 돼요). 주체마다 계산 예산이 걸려요. 미리 돌리는 길은 이것 하나예요 | `editor` |
| `/v1/branches/{branch_id}:align` | POST | 본줄기에 맞추기. 충돌이 없으면 저절로 돼요. 맞추기의 길은 이것 하나예요 | `editor` |
| `/v1/merge-policies` | GET | 위험의 등급표와 자동 병합의 정책(정의의 항목). 고치는 길은 브랜치의 항목(`kind: merge_policy`)이고 느슨하게 하는 것은 `admin`의 승인이 들어요 | 읽기 |
| `/v1/agent-runs/{agent_run_id}/reverts` | POST | 에이전트 실행 하나가 만든 병합 전부를 한 묶음의 되돌리기 제안으로 만들어요(미리 보기와 승인을 거쳐요) | `editor` |

- 승인과 거절은 사람만 해요. MCP의 정의 면은 읽기만이에요. 시스템 계정의 `on_behalf_of`는 관리자가 그 계정에 묶어 둔 사람 가운데서만 고를 수 있어요.
- 위험의 등급은 후보 차이에서 계산하고 항목 종류마다 바닥이 있어요(소스의 `endpoints`, 가리기 규칙, 알림과 목표를 느슨하게, 담당자, 기대 조건의 강도, 비밀 값의 닿는 주소는 R3). 첫 적용의 사람 확인은 R2부터예요.
- 처음 세우기와 혼자 쓰는 시험 배포를 위해 설정 `solo_approval`(`/v1/settings`)을 켜면 자기가 고친 것을 자기가 승인할 수 있고, 모든 승인의 감사에 표시가 남고 건강에 경고가 떠요. 기본은 꺼짐이에요.

## 3. 받기를 넓히는 길(ADR-0008, ingestion-extensions.md)
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| `/v1/sources/{source_id}:push` | POST | **밀어 넣기.** 소스마다 정의로 열어야 해요(기본은 닫힘). 보낸 쪽의 사건 ID로 겹침을 막고(기간 안에서만, 기본 7일, 소스마다 설정) 차례는 `source_time`으로 가려요. 같은 실행기와 안전장치와 행 셈을 지나요 | `ingester`(범위에 든 소스) |
| `/v1/ingest/envelopes` | POST | 수집기가 묶음으로 보내는 길(자원 (1) 3절). `capture_id`와 `seq`로 겹침을 막아요 | `ingester` |
| `/v1/kgs/{kg}/refreshes`, `.../{refresh_id}` | POST, GET | 지정해서 다시 받기와 그 결과(그래프 문서 1-5). 주체마다 한도가 있고 일정에 몫을 남겨요 | `operator`, `viewer` |
| `/v1/kgs/{kg}/value-checks` | POST | 기대와 견주기(그래프 문서 1-5) | 읽기 |
| `/v1/datasets/{dataset_id}:preview` | POST | 견본 줄과 컬럼 통계(자원 (1) 3절) | `editor`, `operator` |
| `/v1/object-types/{type_id}/feed-table` | GET | 타입 쪽에서 보기: 속성마다 어느 공급이 대나, 비어 있나, 얼마나 채워졌나, 배치인가 실시간인가 | 읽기 |
| `/v1/object-types/{type_id}/feed-candidates` | GET | 비어 있는 속성(`attribute`)에서 시작해 댈 수 있는 데이터셋의 후보 | `editor` |
| `/v1/parsers:probe` | POST | 견본 문서를 올려 해석기의 후보와 나무를 봐요. 줄 틀은 견본 줄에서 잡을 곳을 골라 초안을 얻어요 | `editor` |

밀어 넣기만 하는 소스는 빠진 것을 알 수 없어서 맞춰 보기(스냅샷)를 함께 두기를 권하고, 없으면 완전함을 선언하지 못해요. 밀어 넣은 값의 근거는 `declared`예요. 밀어 넣기와 수집 봉투는 둘 다 `ingester`만 하고 그 계정은 읽기가 없어요.

## 4. 비밀 값과 운영
### 4-1. 비밀 값([비밀 값과 암호화](secrets-and-encryption.md))
값을 읽는 길은 없어요. 응답에는 메타 정보(이름, 판, 만든 때, 마지막으로 쓴 때, 만료, 지문의 앞자리, `allowed_hosts`)만 나와요. 읽는 역할과 쓰는 역할은 그 문서 9절이 정해요.

| 길 | 메서드 | 하는 일 | 작업 |
|---|---|---|---|
| `/v1/secrets` | GET, POST | 목록(메타 정보만) / 넣기. `Idempotency-Key` 필요. `allowed_hosts`를 정해요 | - |
| `/v1/secrets/{secret_id}` | GET, PATCH, DELETE | 메타 정보 / 이름, 담당자, 만료, `allowed_hosts`를 고치기(값은 못 고쳐요. `If-Match` 필요) / 지우기(참조하는 소스가 없을 때만) | - |
| `/v1/secrets/{secret_id}/versions` | POST | 바꾸기: 새 판 | - |
| `/v1/secrets/{secret_id}/versions/{version}:check` | POST | 새 판으로 시험 연결 | 작업 |
| `/v1/secrets/{secret_id}/versions/{version}:retire` | POST | 판 물리기 | - |
| `/v1/master-keys` | GET | 으뜸 열쇠의 목록과 상태(재료는 없어요) | - |
| `/v1/master-keys/{key_id}:rewrap`, `:retire` | POST | 다시 감싸기(운영 행위 `rewrap_master_key`라서 `admin`의 승인이 들어요) / 옛 열쇠 물리기 | 작업 |

풀어 쓴 기록은 `GET /v1/audit-records`를 `action`과 `target_id`로 걸러 읽어요.

### 4-2. 운영([프락시움 자신의 관측성](self-observability.md))
| 길 | 방식 | 뜻 | 역할 |
|---|---|---|---|
| `/v1/ops/diagnose` | POST | 진단: 지금 아픈 것, 까닭, 근거 신호, 할 수 있는 운영 행위. 저장소 없이도 답해요. 안전함 | `operator`, `admin`, `auditor` |
| `/v1/ops/actions`, `/v1/ops/actions/{action}` | GET | 운영 행위의 목록(위험의 축 넷, 선언한 효과, 되돌리는 행위, 승인 필요 여부). 행위 하나가 이름 하나예요 | `operator` |
| `/v1/ops/actions/{action}:dry-run` | POST | 미리 해 보기: 하지 않고 무엇이 멈추고 무엇이 걸리는지 답해요(안전함) | `operator` |
| `/v1/ops/actions/{action}/runs` | POST | 운영 행위를 요청해요(실행 `action_run`이 생겨요). `Idempotency-Key`를 받고 시스템 계정의 요청에는 근거가 된 진단의 ID가 있어야 해요. 넓은 행위는 `admin`만 | `operator`(넓은 것은 `admin`) |
| `/v1/ops/action-runs`, `/v1/ops/action-runs/{run_id}` | GET | 실행의 목록과 한 건(상태, 승인, 뒤의 확인) | `operator`, `admin`, `auditor` |
| `/v1/ops/action-runs/{run_id}:approve`, `:reject` | POST | 승인 / 거절. **사람만** 해요: 요청한 쪽이 아닌 `admin`이나 범위를 가진 `operator`. 시스템 계정은 못 해요 | `admin`, `operator` |
| `/v1/ops/action-runs/{run_id}:cancel` | POST | 시작 전이거나 도는 실행을 취소 | 요청한 쪽, `admin` |
| `/v1/ops/rollout` | GET | 올리는 중의 상태(스키마의 옮김과 역할마다의 버전, 어긋남) | `operator`, `admin`, `auditor` |

떠 있나·받을 수 있나·떴나의 길(`/v1/health/live`, `/v1/health/ready`, `/v1/health/started`)은 그래프 문서 5절에 있어요. 설정을 바꾸는 길은 `PATCH /v1/settings`(`update_setting` 행위의 얇은 모양)와 `/v1/settings/{name}/revisions`예요. 같은 미리 해 보기, 승인, 감사를 거쳐요.

## 5. 아직 정하지 않은 것
- 자동으로 멈춘 완전성 선언을 다시 켜는 길과 그 역할.
- 새 시험 KG를 만드는 길(`POST /v1/kgs`)이 필요한지.
- 매핑 제안(`suggest`)의 이름 비슷함 규칙은 `suggest`를 만드는 변경이 닫아요.
- 한 번에 쓰는 길(`items:batch`)이 모두 되거나 모두 안 되는지는 가정이에요.
- `allowed_actions`의 `reason_code`와 오류 `code`(`permission.*`, `blocked.*`)를 하나로 맞출지.

## 6. 관련
ADR-0016(9, 10번), ADR-0014, ADR-0008, ADR-0017, ADR-0018. 요구사항: REQ-229(화면을 위한 API의 원칙), REQ-209(임시 콘솔), REQ-220, REQ-225~228.
