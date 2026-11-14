---
title: REST API의 자원과 경로 (3) — 카탈로그와 사전 검증, 에이전트 시대의 정의, 푸시 수집, 시크릿과 운영
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API의 자원과 경로 (3)

> **경로와 필드는 OpenAPI 문서에서 확정돼요.** 경로는 `/v1/...`이고 액션은 `:액션` 표기 하나예요(rest-api.md 3-8). 자기 관측성의 경로는 [프락시움 자신의 관측성](self-observability.md)이, 시크릿의 경로는 [시크릿과 암호화](secrets-and-encryption.md)가 정한 이름을 따르되 액션 표기는 이 문서와 통일해요.

[자원 (1)](rest-api-resources-data.md), [자원 (2)](rest-api-resources-graph.md)에 이어, 화면이 할 수 있는 일은 모두 공개된 `/v1`로 가능하다는 원칙(ADR-0016 10번)에 따라 추가한 기능을 자원으로 적어요. 임시 콘솔도 이 경로만 호출해요([임시 콘솔](temporary-console.md)). 기능마다 경로가 있는지는 [기능별 경로](rest-api-feature-routes.md)의 표로 확인해요.

## 1. 카탈로그와 사전 검증: 화면이 규칙을 복제하지 않게
### 1-1. 카탈로그(`/v1/catalog/...`)
형식은 JSON 스키마예요. 로그인한 누구나 읽어요. `GET /v1/catalog`가 아래 목록을 반환해요. 이미 다른 자원인 것은 경로를 따로 만들지 않고 그 경로를 가리켜요.

| 항목 | 경로 | 담는 것 |
|---|---|---|
| 정제 연산 | `/v1/catalog/refinement-ops` | 14가지와 연산별 인자 형식 |
| 파서 | `/v1/catalog/parsers` | 파서와 설정 형식 |
| 수집 방식 | `/v1/catalog/receive-modes` | 데이터셋의 `receive_mode`로 고를 수 있는 4가지와 방식별 설정 형식 |
| 링크 의미 | `/v1/catalog/link-meanings` | 11가지와 의미별 규칙 |
| 채널 | `/v1/catalog/channels` | 상태 채널 6종 |
| 값 타입 | `/v1/value-types` | 값 타입(앞 문서 2절. 같은 목록이라 다른 경로를 두지 않아요) |
| 순수 함수 | `/v1/catalog/pure-functions` | 값을 가공하는 고정 함수와 인자 |
| 사전 정의 질의 | `/v1/catalog/queries` | 13가지와 인자 형식 |
| 역할 | `/v1/roles` | 역할 8종(그래프 문서 4절. 같은 목록) |
| 오류 코드 | `/v1/catalog/error-codes` | `code`와 의미(`/v1/problems/{code}`가 한 건의 설명) |
| 이벤트 코드 | `/v1/catalog/event-codes`, `/v1/catalog/event-codes/{code}` | 의미, 심각도, 확인할 항목, 실행할 수 있는 운영 액션. `catalog_version`이 있어요 |
| 차단 이유 코드 | `/v1/catalog/reason-codes` | `allowed_actions`의 `reason_code` |
| 기본값 | `/v1/catalog/defaults` | 기본값(설정)의 목록 |

### 1-2. 저장하지 않는 검증과 미리보기
| 경로 | 방식 | 설명 | 역할 |
|---|---|---|---|
| `/v1/definition-items:validate` | POST | 항목(`kind`와 `body`, 새 항목이면 `item_id`는 비워요)을 저장하지 않고 검사해요. 필드별 오류와 수정 방법의 힌트를 줘요. `branch_id`를 받아요 | 그 항목을 읽는 권한 |
| `/v1/definition-items:preview` | POST | 저장하지 않고 미리 봐요. `kind: feed`이면 샘플 행 몇 개가 어떤 객체와 링크가 되는지(식별이 기존 객체에 붙는지 새로 만드는지, 타입이 맞지 않아 탈락하는 값) | 같음. 다른 소스를 읽는 정의이면 읽는 소스의 범위도 있어야 해요 |
| `/v1/definition-items:suggest` | POST | 정해진 규칙으로 초안을 만들어요(같은 입력이면 같은 응답). `kind: feed`이면 컬럼과 속성의 쌍과 식별 후보를 이유와 함께. 자동으로 적용하지 않아요 | `editor` |
| 모든 자원의 `meta.allowed_actions` | 응답의 필드 | 지금 이 주체가 할 수 있는 것과 차단 이유 코드(rest-api.md 4절) | |

### 1-3. 일관된 목록, 일괄 조회, 시각화에 적합한 형식
- 목록은 필터링, 정렬, 필드 선택(`fields`), 함께 적재(`include`), 커서 페이지네이션을 일관되게 지원하고 전체 개수는 요청하면 줘요.
- 여러 건을 한 번에 읽기: `POST /v1/kgs/{kg}/objects:batch-get`, `POST /v1/definition-items:batch-get`(안전함). 한 번에 쓰기: `POST /v1/branches/{branch_id}/items:batch`.
- 이웃과 경로와 영향의 응답은 시각화에 적합한 형식(노드와 링크의 목록, 그룹 요약, 더 펼칠 수 있는 위치 표시)으로도 줘요(`shape=graph`). 큰 응답은 그룹으로 묶어 요약한 뒤 펼치는 경로를 줘요.
- 작업의 진행, 실행의 단계, 헬스 상태와 문제, KG의 변경은 이벤트 스트림으로 내보내요(rest-api.md 3-10).
- 타입, 속성, 링크, 액션, 코드의 표시 이름과 설명은 언어별로 담을 필드(`display_names`)가 있어요.

## 2. 에이전트 시대의 정의(ADR-0014, definitions-at-agent-scale.md)
| 경로 | 방식 | 설명 | 역할 |
|---|---|---|---|
| 브랜치와 제안의 필드 | | `authored_by`, `on_behalf_of`, `agent_run_id`, `agent_build`, `trigger_ref`, `evidence`, `intent`, `decision_log_hash`, `risk_grade`, `after` | 작성하는 쪽 |
| `/v1/proposals/{id}/checks`, `/v1/proposals/{id}/candidate-runs` | POST | 큐가 실행할 검사와 드라이 런을 미리 실행해요(초안 제안에서도 가능해요). 주체별로 계산 예산이 적용돼요. 미리 실행하는 경로는 이것 하나예요 | `editor` |
| `/v1/branches/{branch_id}:align` | POST | 메인 브랜치 최신화. 충돌이 없으면 자동으로 돼요. 정렬하는 경로는 이것 하나예요 | `editor` |
| `/v1/merge-policies` | GET | 위험 등급표와 자동 병합 정책(정의 항목). 수정하는 경로는 브랜치의 항목(`kind: merge_policy`)이고 규칙을 완화하려면 `admin`의 승인이 필요해요 | 읽기 |
| `/v1/agent-runs/{agent_run_id}/reverts` | POST | 에이전트 실행 하나가 만든 병합 전부를 배치 하나의 리버트 제안으로 만들어요(미리 보기와 승인을 거쳐요) | `editor` |

- 승인과 거절은 사람만 해요. MCP의 정의 인터페이스는 읽기만이에요. 서비스 계정의 `on_behalf_of`는 관리자가 그 계정에 연결해 둔 사람 중에서만 선택할 수 있어요.
- 위험 등급은 후보 차이에서 계산하고 항목 종류마다 하한이 있어요(소스의 `endpoints`, 가리기 규칙, 알림과 목표의 완화, 담당자, 기대 조건의 강도, 시크릿의 접속 주소는 R3). 첫 적용의 사람 확인은 R2부터예요.
- 초기 구성과 단독 사용 테스트 배포를 위해 설정 `solo_approval`(`/v1/settings`)을 켜면 자기가 수정한 것을 자기가 승인할 수 있고, 모든 승인의 감사 로그에 표시가 남고 헬스 상태에 경고가 표시돼요. 기본은 꺼짐이에요.

## 3. 수집 확장(ADR-0008, ingestion-extensions.md)
| 경로 | 방식 | 설명 | 역할 |
|---|---|---|---|
| `/v1/sources/{source_id}:push` | POST | **푸시 수집.** 소스마다 정의로 열어야 해요(기본은 닫힘). 보낸 쪽의 이벤트 ID로 중복을 막고(기간 안에서만, 기본 7일, 소스마다 설정) 순서는 `source_time`으로 가려요. 같은 실행기와 안전장치와 건수 정합성을 거쳐요 | `ingester`(범위에 속한 소스) |
| `/v1/ingest/envelopes` | POST | 수집기가 배치로 보내는 경로(자원 (1) 3절). `capture_id`와 `seq`로 중복을 막아요 | `ingester` |
| `/v1/kgs/{kg}/refreshes`, `.../{refresh_id}` | POST, GET | 지정 재수집과 그 결과(그래프 문서 1-5). 주체별로 한도가 있고 스케줄용 할당량을 남겨요 | `operator`, `viewer` |
| `/v1/kgs/{kg}/value-checks` | POST | 기대와 대조(그래프 문서 1-5) | 읽기 |
| `/v1/datasets/{dataset_id}:preview` | POST | 샘플 행과 컬럼 통계(자원 (1) 3절) | `editor`, `operator` |
| `/v1/object-types/{type_id}/feed-table` | GET | 타입 기준 보기: 속성마다 어느 피드가 채우는지, 비어 있는지, 얼마나 채워졌는지, 배치인지 실시간인지 | 읽기 |
| `/v1/object-types/{type_id}/feed-candidates` | GET | 비어 있는 속성(`attribute`)에서 시작해 채울 수 있는 데이터셋의 후보 | `editor` |
| `/v1/parsers:probe` | POST | 샘플 문서를 올려 파서 후보와 트리를 확인해요. 라인 템플릿은 샘플 행에서 추출할 위치를 골라 초안을 얻어요 | `editor` |

푸시 수집만 하는 소스는 빠진 것을 알 수 없어서 전체 스냅샷 정합성 대조를 함께 두기를 권하고, 없으면 완전함을 선언하지 못해요. 푸시된 값의 근거는 `declared`예요. 푸시 수집과 수집 엔벨로프는 둘 다 `ingester`만 할 수 있고 그 계정은 읽기 권한이 없어요.

## 4. 시크릿과 운영
### 4-1. 시크릿([시크릿과 암호화](secrets-and-encryption.md))
값을 읽는 경로는 없어요. 응답에는 메타 정보(이름, 버전, 생성 시각, 마지막 사용 시각, 만료, 지문 앞부분, `allowed_hosts`)만 나와요. 읽는 역할과 쓰는 역할은 그 문서 9절이 정해요.

| 경로 | 메서드 | 하는 일 | 작업 |
|---|---|---|---|
| `/v1/secrets` | GET, POST | 목록(메타 정보만) / 등록. `Idempotency-Key` 필요. `allowed_hosts`를 정해요 | - |
| `/v1/secrets/{secret_id}` | GET, PATCH, DELETE | 메타 정보 / 이름, 담당자, 만료, `allowed_hosts` 수정(값은 수정할 수 없어요. `If-Match` 필요) / 삭제(참조하는 소스가 없을 때만) | - |
| `/v1/secrets/{secret_id}/versions` | POST | 교체: 새 버전 | - |
| `/v1/secrets/{secret_id}/versions/{version}:check` | POST | 새 버전으로 테스트 연결 | 작업 |
| `/v1/secrets/{secret_id}/versions/{version}:retire` | POST | 버전 폐기 | - |
| `/v1/master-keys` | GET | 마스터 키의 목록과 상태(키 재료는 없어요) | - |
| `/v1/master-keys/{key_id}:rewrap`, `:retire` | POST | 다시 래핑(운영 액션 `rewrap_master_key`라서 `admin`의 승인이 필요해요) / 이전 키 폐기 | 작업 |

복호화 사용 이력은 `GET /v1/audit-records`를 `action`과 `target_id`로 필터링해 읽어요.

### 4-2. 운영([프락시움 자신의 관측성](self-observability.md))
| 경로 | 방식 | 설명 | 역할 |
|---|---|---|---|
| `/v1/ops/diagnose` | POST | 진단: 현재 이상 상태인 것, 원인, 근거 신호, 실행할 수 있는 운영 액션. 스토리지 없이도 답해요. 안전함 | `operator`, `admin`, `auditor` |
| `/v1/ops/actions`, `/v1/ops/actions/{action}` | GET | 운영 액션의 목록(위험 축 4개, 선언한 효과, 롤백 액션, 승인 필요 여부). 액션 하나가 이름 하나예요 | `operator` |
| `/v1/ops/actions/{action}:dry-run` | POST | 미리 실행: 실제로 실행하지 않고 무엇이 중단되고 무엇이 영향받는지 응답해요(안전함) | `operator` |
| `/v1/ops/actions/{action}/runs` | POST | 운영 액션을 요청해요(실행 `action_run`이 생성돼요). `Idempotency-Key`를 받고 서비스 계정의 요청에는 근거가 된 진단의 ID가 있어야 해요. 범위가 넓은 액션은 `admin`만 | `operator`(넓은 것은 `admin`) |
| `/v1/ops/action-runs`, `/v1/ops/action-runs/{run_id}` | GET | 실행의 목록과 한 건(상태, 승인, 사후 확인) | `operator`, `admin`, `auditor` |
| `/v1/ops/action-runs/{run_id}:approve`, `:reject` | POST | 승인 / 거절. **사람만** 할 수 있어요: 요청한 쪽이 아닌 `admin`이나 범위를 가진 `operator`. 서비스 계정은 할 수 없어요 | `admin`, `operator` |
| `/v1/ops/action-runs/{run_id}:cancel` | POST | 시작 전이거나 실행 중인 작업을 취소 | 요청한 쪽, `admin` |
| `/v1/ops/rollout` | GET | 롤아웃 상태(스키마 마이그레이션과 역할별 버전, 불일치) | `operator`, `admin`, `auditor` |

live·ready·started 경로(`/v1/health/live`, `/v1/health/ready`, `/v1/health/started`)는 그래프 문서 5절에 있어요. 설정을 변경하는 경로는 `PATCH /v1/settings`(`update_setting` 액션의 축약 형태)와 `/v1/settings/{name}/revisions`예요. 같은 미리 실행, 승인, 감사를 거쳐요.

## 5. 미결 사항
- 자동으로 중단된 완전성 선언을 다시 켜는 경로와 그 역할.
- 새 테스트 KG를 만드는 길(`POST /v1/kgs`)이 필요한지.
- 매핑 제안(`suggest`)의 이름 유사도 규칙은 `suggest`를 구현하는 변경이 확정해요.
- 한 번에 쓰는 경로(`items:batch`)가 모두 성공하거나 모두 실패하는지는 가정이에요.
- `allowed_actions`의 `reason_code`와 오류 `code`(`permission.*`, `blocked.*`)를 하나로 통일할지.

## 6. 관련
ADR-0016(9, 10번), ADR-0014, ADR-0008, ADR-0017, ADR-0018. 요구사항: REQ-229(화면을 위한 API의 원칙), REQ-209(임시 콘솔), REQ-220, REQ-225~228.
