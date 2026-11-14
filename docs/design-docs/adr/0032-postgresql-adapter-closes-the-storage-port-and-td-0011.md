---
title: ADR-0032 PostgreSQL 어댑터가 스토리지 포트를 채운다 — 드라이버 lib/pq, 직접 만든 마이그레이션 실행기, 진짜 서버로 하는 테스트
kind: decision
status: active
enforced_by:
  - GOARCH020
  - GOARCH010
  - GODEP010
  - GODEP011
  - GODEP012
  - GOCOV001
  - GOCOV003
  - GOTEST002
  - TRACE006
  - TLOCK001
  - backend/internal/pgstore/graphcheck_test.go
  - backend/internal/pgstore/internal_test.go
  - backend/internal/pgstore/store_test.go
  - backend/internal/pgstore/damage_test.go
  - backend/cmd/praxium/store_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/postgres-operations.md
  - docs/design-docs/rest-api.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
  - docs/design-docs/kg-storage.md
  - docs/product-specs/requirements.md
touches_protected:
  - .github/workflows/ci.yml
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
  - docs/product-specs/requirements.md
decision_hash: b36027a06cf96e11
---
# ADR-0032 PostgreSQL 어댑터가 스토리지 포트를 채운다

## 맥락

스토리지 포트(`Catalog`, `Writer`, `Reader`, `Feed`)와 적합성 테스트 31사례(`adapter.graphcheck`)는 ADR-0025가 세웠고, 구현은 인메모리 어댑터(`adapter.memstore`) 하나였다. 그래서 `api serve`는 서버가 끝나면 KG가 사라지고, 시작할 때마다 `-dev-truth`로 세계를 다시 적재해야 했다(부채 TD-0011). 운영 수준의 스토리지는 PostgreSQL이고, 설계는 [KG의 저장과 버전](../kg-storage.md)과 [스토리지의 운영 수준](../postgres-operations.md)에 있다. 이 결정은 그 설계의 알파 몫 가운데 포트를 채우는 데 필요한 것을 구현하고, 그 과정에서 정해야 했던 것(드라이버, 마이그레이션, 테스트 방법, 외부 모듈의 검증)을 기록한다.

## 결정

1. **어댑터.** 새 단위 `adapter.pgstore`(`backend/internal/pgstore`)가 `graph.Store`를 구현한다. 적합성 테스트 31사례를 고치지 않고 통과한다. `may_import`는 `core.kernel`, `core.model`(행이 가진 등급과 근거의 타입), `core.graph`이고 `test_may_import`는 `adapter.graphcheck`다. 어댑터는 다른 어댑터를 import하지 않고, 조립은 `cmd.praxium`만 한다.
2. **드라이버는 `github.com/lib/pq` v1.12.3(MIT) 하나다.** 표준 `database/sql`의 드라이버이고 다른 모듈에 기대지 않는다(전이 의존 0). `pgx`와 다른 라이브러리는 들이지 않는다. 연결은 `pq.NewConnector`와 `sql.OpenDB`로 만든다. 구조 계약의 `go.third_party`에 `github.com/lib/pq: [adapter.pgstore]`를 적고 `ARCHITECTURE.md`의 모듈 표에 줄을 더한다. 그 모듈만 `adapter.pgstore`에서 쓸 수 있다(`GOARCH020`).
3. **외부 모듈의 검증: `go.sum`을 공식 체크섬 데이터베이스와 CI에서 견준다.** 이 모듈은 개발 환경에서 프록시와 체크섬 데이터베이스에 닿지 못해 리포지토리에서 직접(`GOPROXY=direct GONOSUMDB=github.com/lib/pq`, 태그 v1.12.3) 받았다. 그래서 `backend/go.sum`의 두 줄은 데이터베이스가 확인한 적이 없다. 이를 메우려고 CI에 `go-sum` 작업을 두고 `gate`가 기다린다. `scripts/verify-go-sum.sh`는 `backend/`를 임시 폴더에 복사해 `go.sum`을 지우고, 빈 모듈 캐시에서 기본 프록시와 기본 데이터베이스로 `go mod tidy`와 `go mod verify`를 돌려 `go.sum`과 `go.mod`가 커밋된 것과 같은지 견준다. 데이터베이스가 보증하지 않는 줄이면 새 줄이 만들어지지 않거나 달라서 실패한다. 이 견주기는 이 환경에서 돌릴 수 없어 CI의 첫 실행이 처음의 확인이다(스크립트의 쓰임새와 틀린 줄을 잡는 것은 로컬 프록시로 확인했다).
4. **마이그레이션은 직접 만든 실행기로 한다(도구 의존 없음).** 파일 `migrations/NNNN_이름.sql`을 `go:embed`로 내장한다. 번호는 1부터 빈틈이 없어야 하고(어기면 기동에서 실패), 적용한 파일은 SHA-256으로 기록해 고쳐 쓰면 `conflict`로 거부한다(고치는 대신 새 파일을 더한다). 한 파일은 한 트랜잭션이고 트랜잭션 안의 어드바이저리 잠금(advisory lock)으로 한 번에 하나만 돈다. 잠금 대기 한도에 걸리면 물러서며 다섯 번까지 다시 한다. 파일 머리의 `-- praxium:lock_timeout=3s`, `-- praxium:statement_timeout=60s`로 그 파일의 한도를 정한다(다른 지시어는 오류다). 규칙: 단계는 넓히는 쪽(expand)만 하고, 좁히는 변경은 코드가 그것을 쓰지 않게 된 다음 버전에 한다. 이 규칙은 리뷰가 지키고 기계가 검사하지는 않는다. 바이너리보다 새로운 번호가 데이터베이스에 있어도(롤링 배포) 기동은 막지 않는다. `Open`은 언제나 마이그레이션을 먼저 돌린다.
5. **쓰기의 순서와 스키마.** `ApplyChunk`, `AbandonApply`, `Seal`은 한 트랜잭션에서 헤드 행을 잠그고(`FOR UPDATE`) 세대, 리스, 엔진 의미 버전, 기대한 헤드의 순으로 확인한 뒤 모든 행을 쓰고 헤드를 옮긴다. 변경 번호(`applied_seq`)는 헤드 행의 카운터이고 시퀀스가 아니다. `readable_through`는 헤드가 정한 값이 오를 때만(마지막 청크와 함께) 오르므로 읽는 쪽은 아직 읽을 수 없는 청크의 행을 보지 못한다. 열린 행은 `*_live`, 닫힌 행은 `*_hist`에 두고(`object`와 `link`), 행은 `seq_from <= S < seq_to`일 때 S에서 유효하다. **모든 기본키와 인덱스는 `kg_id`로 시작한다.** 외래 키는 없고, 확인은 헤드를 잠근 한 트랜잭션 안에서 쓰는 쪽이 한다. 한 KG의 읽기는 읽기 전용, 반복 읽기(repeatable read) 트랜잭션이고 스냅샷의 세대가 바뀌면(복원) `generation_changed`다. KG의 이름이 유일하고 운영 KG는 하나라는 규칙은 KG를 가로지르므로 `kg_id`로 시작하는 유일 인덱스로 둘 수 없어, 트랜잭션 수준의 어드바이저리 잠금 아래에서 만들 때 검사한다. 세션 잠금과 LISTEN/NOTIFY는 쓰지 않으며 `WaitReadable`은 헤드를 짧은 간격(기본 50ms)으로 읽는다.
6. **라우팅 계층.** 스토리지의 모든 접근은 `kg_id`를 받아 데이터베이스를 찾는 `router`를 지난다. 지금은 답이 하나(`singleRouter`)다. KG 목록과 이름 찾기는 모든 데이터베이스를 훑는다.
7. **역할별 한도.** 역할(`applier`, `api_read`, `bulk`, `dry_run`, `migration`)마다 문 시간(`statement_timeout`), 잠금 대기(`lock_timeout`), 트랜잭션 안에서 노는 시간(`idle_in_transaction_session_timeout`)이 있고 트랜잭션 안에서 `set_config(..., true)`(`SET LOCAL`)로 정하므로 풀의 다른 사용에 새지 않는다. 기본값은 가정이고 `Config.Limits`로 바꾼다. 한도에 걸린 오류는 `internal`이고 메시지에 "time limit"이 있다. 새 오류 코드는 더하지 않는다.
8. **저장하는 모양.** 행의 속성과 출처 목록은 JSONB, 해시는 `bytea`, 시각은 UTC 마이크로초로 자른다. 객체와 링크의 ID는 **`text COLLATE "C"`로** 저장한다. 설계 문서의 스케치(16바이트)와 다른 점이고 스케치를 이 결정에 맞춰 고쳤다. 이유는 이렇다. 포트의 ID는 접두어가 있는 문자열이고 정렬은 바이트 순서여야 하므로(스캔의 `after`), 변환 없이 같은 순서가 나오는 열을 골랐다. 크기가 문제가 되면 닫힌 쪽에서 바꾸는 후속 마이그레이션으로 간다. 설계 문서의 `scope_v1`~`scope_v4` 열은 포트에 범위가 없어 이 변경에 없고 접근 제어 작업의 몫이다(스토리지 설계 문서에 한 줄로 적었다).
9. **기동 코드.** `praxium api serve`가 `-store memory|postgres`(기본 `memory`, 기존 동작 그대로)와 `-store-dsn-file <파일>`을 받는다. **연결 문자열(DSN)은 파일에서만 읽는다**: 인자와 환경 변수에는 두지 않고, 어댑터의 `Config`는 출력에서 DSN을 가리며, 오류와 로그에 DSN과 비밀번호가 나오지 않는다. `-dev-truth`는 어느 스토리지에든 적재하며 이름 `dev`인 KG가 이미 있으면 거부된다. 운영 모드는 진짜 인증기(TD-0010)가 없어 계속 기동을 거부하고, 오류가 운영 스토리지는 이미 있다는 것(TD-0011)을 함께 말한다.
10. **테스트는 진짜 PostgreSQL 16에서 한다.** 환경 변수 `PRAXIUM_TEST_PG_DSN`이 서버를 가리킨다. 변수가 없으면 건너뛰지 않고 실패하며(조용히 통과하는 경로를 두지 않는다), 메시지가 쓰는 법을 말한다. 테스트마다 새 데이터베이스를 `CREATE DATABASE`로 만들고 끝나면 `DROP DATABASE ... WITH (FORCE)`한다. 기동 코드의 `os/exec`는 `restricted_stdlib`가 막으므로(그 목록은 바꾸지 않았다) 서버는 테스트 바깥에서 준비한다. 로컬은 `scripts/with-postgres.sh <명령>`이 일회용 서버를 소켓에만 띄우고(root이면 비특권 사용자로) 변수를 정해 명령을 돌린 뒤 지운다. CI는 `test` 작업의 서비스 컨테이너 `postgres:16`이 변수를 채운다. 테스트에는 빌드 제약을 달지 않는다(`GOARCH055`). 단위 `go:adapter.pgstore`는 같은 커버리지 하한 규칙을 따른다(`coverage-floor.json`, 90 이상).

### 바뀐 설정과 보호 경로

| 경로 | 바뀐 것 | 이유 |
|---|---|---|
| `harness/config/architecture.yaml` | 단위 `adapter.pgstore` 등록, `third_party`에 `github.com/lib/pq` | 새 단위와 승인된 새 의존 등록 |
| `harness/config/coverage-floor.json` | `go:adapter.pgstore`의 하한 91.5 추가, `go:cmd.praxium`의 하한 94에서 94.5로(둘 다 `pnpm ratchet`이 쓴다) | 새 단위의 하한과 측정값을 따라가는 올림. 내린 하한은 없다 |
| `harness/config/test-lock.json` | 새 테스트의 지문(`pnpm lock-tests`가 쓴다) | 새 테스트의 잠금. 기존 항목은 바뀌지 않았다 |
| `.github/workflows/ci.yml` | `test` 작업에 서비스 컨테이너와 변수, `go-sum` 작업, `gate`가 그것을 기다린다 | 결정 3과 10 |
| `docs/product-specs/requirements.md` | REQ-270 추가, REQ-260 고침 | 요구사항 표의 변화 |

규칙, 한도, 허용 목록, 잠긴 테스트는 느슨하게 하지 않았다. `restricted_stdlib`와 다른 단위의 하한과 기존 테스트는 그대로다.

## 이유

- 포트와 적합성 테스트가 이미 있어서 구현이 맞는지는 31사례가 판가름한다. 같은 사례를 진짜 서버에서 돌리는 것이 이 어댑터의 가장 믿을 만한 증거다.
- `lib/pq`는 하나의 모듈이고 전이 의존이 없다. 쓰는 기능(준비된 문, 트랜잭션, 배열 인자, 오류 코드, SCRAM 인증)을 모두 가진다. `pgx`는 기능이 많지만 모듈이 여럿이고 이 변경이 필요로 하지 않는다. 와이어 프로토콜과 인증을 직접 짜는 것은 안전하게 오래 유지하기 어렵다(ARCHITECTURE.md의 표에 이유로 적었다).
- 마이그레이션 도구는 그 자체가 모듈이거나 별도 실행 파일이고, 이 어댑터에 필요한 것은 번호, 체크섬, 잠금, 한도 정도라서 이백 줄 남짓한 실행기로 충분하다. 설계 문서의 "트랜잭션 단위 풀에서 깨지는 것(세션 잠금, 듣기)을 쓰지 않는다"를 지키려면 트랜잭션 안의 잠금이 필요하고 도구가 그것을 보장하는지 알기 어렵다.
- 변경 번호를 헤드 행의 카운터로 두면 커밋의 순서와 번호의 순서가 같고(시퀀스는 구멍과 순서 뒤바뀜이 있다), 되감기(포기)가 번호를 되쓰는 의미를 그대로 둔다.
- 모든 키가 `kg_id`로 시작하면 KG를 다른 데이터베이스로 옮기는 경로(설계 문서 13절)이 열려 있다. 외래 키가 없는 것은 그 이동과 대량 쓰기의 비용 때문이고, 검사는 쓰는 한 트랜잭션이 맡는다.
- 서버를 흉내 내는 대신 진짜 서버를 쓰는 것은 잠금, 격리, 오류 코드가 어댑터의 의미의 일부이기 때문이다. 건너뛰기를 막는 것은 서버가 없는 환경에서 초록불이 거짓이 되는 것을 막기 위해서다.
- `go.sum`을 데이터베이스와 CI에서 견주는 것은, 이 환경에서 데이터베이스를 거치지 못한 것을 미루지 않고 기계가 검사하게 하는 길이다.

## 대안과 버린 이유

| 대안 | 버린 이유 |
|---|---|
| `pgx` | 모듈이 여럿이라 "외부 모듈은 하나만" 원칙에 안 맞고, 이 변경이 필요한 기능은 `lib/pq`에 있다 |
| `golang-migrate`, `goose` 같은 도구 | 새 의존이고 트랜잭션 안의 잠금과 한도를 우리 규칙대로 쓰기 어렵다 |
| `testcontainers` 또는 테스트 코드가 서버를 띄우기 | 새 의존이고, `os/exec` 제한(`restricted_stdlib`)을 풀어야 한다. 그 목록은 바꾸지 않는다 |
| 서버가 없으면 테스트를 건너뛰기 | 서버 없는 환경에서 거짓 초록불이 된다 |
| 변경 번호에 시퀀스 | 커밋의 순서와 번호의 순서가 달라지고 구멍이 생긴다. 되감기와도 맞지 않는다 |
| ID를 `bytea` 16바이트로 | 포트의 ID는 문자열이고 변환과 정렬 규칙이 새 위험이다. 크기 문제는 측정 뒤로 미룬다. 설계 문서의 스케치는 이 결정에 맞춰 고쳤다 |
| DSN을 인자나 환경 변수로 | 프로세스 목록과 로그에 비밀이 남는다 |

## 결과

- TD-0011을 닫는다(`status: done`). 열린 부채는 12에서 11로 줄고 새 부채는 등록하지 않았다.
- `api serve -store postgres`로 서버가 다시 기동해도 KG가 남는다. 기본은 인메모리 스토리지다.
- 새 요구사항 REQ-270이 서고 REQ-260이 운영 스토리지가 이미 있음을 말하게 고쳐졌다.
- **한계.** (1) 이 변경은 PostgreSQL 16에서만 돌려 보았다. (2) 한도의 기본값과 연결 풀 크기는 가정이고 클로즈드 베타에서 잰다. (3) 라우팅은 데이터베이스가 하나인 경우만 있다. (4) `dry_run`과 `bulk` 역할은 한도의 틀만 있고 쓰는 경로는 아직 없다. (5) 복제본 읽기, 파티션, 보관, 신호, `kg_maintenance`와 `raw_record` 같은 [알파] 항목은 이 변경이 하지 않는다. (6) `-store-dsn-file`의 파일 권한은 검사하지 않는다. (7) 서버와의 TLS는 DSN의 `sslmode`에 위임한다. (8) `go-sum` 작업의 데이터베이스 견주기는 CI에서 처음 돈다.

## 요구사항 표의 변화

- REQ-270 추가: PostgreSQL 어댑터와 그 테스트의 방법.
- REQ-260 고침: `-store postgres`를 말하고, 운영 모드 거부의 이유에서 운영 스토리지를 뺀다.
