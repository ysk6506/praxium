---
title: ADR-0033 파일 소스 하나를 KG까지 싣는 수집의 첫 한 줄
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH021
  - GOCOV001
  - TRACE006
  - backend/internal/ingest/pipeline_test.go
  - backend/internal/filesource/source_test.go
  - backend/internal/ingestcheck/accept_test.go
  - backend/cmd/praxium/ingest_test.go
updates_docs:
  - ARCHITECTURE.md
  - AGENTS.md
  - docs/exec-plans/active/plan.md
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
  - docs/product-specs/requirements.md
---
# ADR-0033 파일 소스 하나를 KG까지 싣는 수집의 첫 한 줄

## 맥락

KG를 채우는 길은 개발용 적재기(`adapter.devload`)뿐이었다. 수집 설계(ADR-0008)는 단계와 각 단계의 산출물과 경계를 정했지만 구현이 없었다. 계획의 7번("얇게 끝까지 한 줄")이 이 빈 곳을 가리킨다. 이 결정은 파일 소스 하나만으로 수집, 파싱, 원본 레코드의 변경, 정제, 피드(매핑), 식별, 링크, 적용을 끝까지 잇는 첫 구현의 범위와 단위를 정한다.

## 결정

**범위.** 소스는 파일 하나의 종류이고 형식은 CSV와 JSON Lines다. 소스 정의와 매핑 정의는 JSON 데이터이고(서드파티 파서 없음) 타입 등록부(표준 모델의 타입, 링크, 식별 키, 식별자 체계)로 검증한다. 틀린 정의는 실행 전에 거절한다.

**단위.**

| 단위 | 하는 일 |
|---|---|
| `core.ingest` | 정의, 계획(`Compile`), 단계, 상태 포트(`StateStore`), 보고. `core.kernel`, `core.model`, `core.graph`만 읽는다 |
| `adapter.filesource` | `Fetcher` 포트의 파일 구현. 열기 함수를 시작점에서 받고 쓰는 길이 없다 |
| `adapter.ingestcheck` | 테스트 전용 수용 검사(생성기의 세계 → 파일 → 파이프라인 → 개발용 적재기와 견주기) |
| `cmd.praxium` | `ingest run`. 파일을 여는 곳은 여기뿐이다 |

**구현한 것.**

- 단계마다 산출물을 따로 둔다. 원본 레코드는 받은 그대로 구간(`from`, `to`)을 달아 상태에 두고, 행마다 추가·수정·그대로·삭제를 센다. 정제의 닫힌 연산은 `filter`, `fill_empty`, `replace` 셋이다.
- 피드는 객체를 짓는 피드와 값만 붙이는 피드로 나뉜다. 속성은 `rank`(낮은 쪽), 나중 수집, 피드 차례로 하나가 이기고 이기지 못한 다른 값은 `source_conflict`로 알린다.
- 식별은 표준 모델의 식별 키(체계 키, 속성 조합 키)로 결정적으로 한다. 서로 다른 모양의 두 파일에서 온 같은 객체는 하나가 된다. 객체 ID는 처음 만든 키로 정하고 키는 상태에 계속 남아 되살아날 때 같은 ID를 쓴다.
- 링크는 두 피드의 키가 같은 행끼리 잇고(`same_key`), 대상이 없거나 여럿이거나 차수 규칙을 깨면 문제로 알리고 건너뛴다.
- 적용은 `core.graph`의 적용기를 그대로 쓴다. 값에는 피드의 `basis`와 근거 등급(사실), 근거 참조(소스, 데이터셋, 행 키)를 단다. 같은 입력의 둘째 실행은 `Seal`이 마지막 버전을 돌려주므로 새 버전이 나오지 않는다.
- 사라짐: 완전한 수집이 3번 잇달아 행을 못 보고 처음 안 보인 때부터 24시간이 지나야 원본 레코드를 닫는다(기본값 표). 닫힌 행의 객체는 `retires_objects` 피드가 본 것일 때만 링크와 함께 물러난다. 덜 받은 수집(크기 한도, 읽기 오류)은 쓰지 않고 사라짐으로 세지 않는다. 물러난 객체의 식별자는 닫지 않아서 같은 행이 돌아오면 같은 ID로 되살아난다.
- 나쁜 행(식별자 없음, 값 변환 실패, 읽을 수 없는 줄, 겹친 키)은 사유와 함께 격리해 보고하고 나머지는 이어서 적용한다. 받은 열이 선언과 어긋나면 그 데이터셋의 수집을 쓰지 않는다(`shape_changed`). 행 수는 받은 수 = 읽은 수 + 못 읽은 수, 읽은 수 = 쓰임 + 걸러 냄 + 문제 + 안 쓰임으로 맞춘다.

**미룬 것.** 일정, 흐름 소스, 푸시 수집과 엔벨로프, 소스 등록부와 REST 길, 정제의 나머지 연산, 매핑 보조(`suggest`), 객체 병합과 분리, 대량 사라짐과 처음 적용의 안전장치(덜 받은 수집과 모양 바뀜만 있다), 붙을 곳 없는 값의 24시간 대기(바로 문제로 알린다), 컨텍스트 키와 집합 키(`located_in`+`name` 같은)의 식별, HTTP와 SQL 어댑터, 값마다 규칙까지 따라가는 출처 추적, 상태의 PostgreSQL 저장.

**설계와 다른 점.**

- 원본 레코드의 구간은 순서 확정 단계(`ingest_seq`)가 아니라 실행 번호로 센다. 순서 확정 단계는 흐름 소스와 함께 온다.
- 안 보인 횟수는 (피드, 객체)가 아니라 (데이터셋, 자연 키)마다 센다. 시간은 `tick`이 아니라 주입한 시계로 잰다.
- 값이 "알 수 없음"인 것은 등급을 달지 않고 값이 없는 것으로 둔다.
- 링크의 `basis` 기본은 `derived`이고 매핑이 바꿀 수 있다.
- 컨텍스트 키가 필요한 `Rack`은 약한 키 `asset_tag`를 쓴다. 이 때문에 객체 ID가 개발용 적재기와 다르고, 견주기는 이름과 일련번호 같은 맞춤 키로 한다.
- 계약(`planned/routes.json`)에 있는 수집의 길(`/v1/sources/{source_id}/uploads` 등)은 소스 등록부와 작업과 인증이 있어야 하므로 하나도 구현하지 않았다. 이 슬라이스에서는 계약을 바꾸지 않고, 길은 `planned`에 그대로 있다.

## 이유

설계 전체를 기다리지 않고 한 줄을 끝까지 이어야 단계의 경계와 산출물이 맞는지 확인된다. 단위를 `core.ingest`(순수한 계산)와 `adapter.filesource`(파일)로 가른 것은 이후 HTTP와 SQL 어댑터가 같은 포트를 쓰게 하기 위해서다. 요구를 새로 쪼개(REQ-263부터 REQ-269) 구현한 몫만 표시하고 나머지 예정 요구는 그대로 둔다(ADR-0025의 방식).

## 대안과 버린 이유

- 개발용 적재기를 넓히는 길: 적재기는 정답을 곧바로 싣는 편의 도구이고 수집의 경계(원본 레코드, 매핑, 식별)를 시험하지 못한다.
- 정제와 매핑을 코드로 두는 길: 정의는 데이터이고 사용자 코드를 받지 않는다는 설계(REQ-115)에 어긋난다.
- 서드파티 CSV, JSON 파서: 새 의존성은 승인 없이 더하지 않는다. 표준 라이브러리로 충분했다.
- REST 길을 일부만 구현: 소스 등록부와 작업과 인증 없이 올리는 길은 계약의 뜻을 지키지 못한다.

## 결과

- `praxium ingest run`으로 소스 정의, 매핑 정의, 파일을 주면 실행 하나가 KG에 적용되고 단계별 수와 문제가 나온다. 저장소는 명령이 사는 동안만 있다(메모리).
- 보호 경로 변경: `harness/config/architecture.yaml`(새 유닛 셋 등록), `harness/config/coverage-floor.json`(새 유닛의 하한), `harness/config/test-lock.json`(새 테스트 잠금), `docs/product-specs/requirements.md`(REQ-263~269), `AGENTS.md`(실행 예). 기존 유닛의 `may_import`와 한도는 바꾸지 않았다.
- 남은 일은 위 "미룬 것"이다. 안 보임 판정과 상태의 영속은 PostgreSQL 어댑터(TD-0011)와 함께 이어진다.
