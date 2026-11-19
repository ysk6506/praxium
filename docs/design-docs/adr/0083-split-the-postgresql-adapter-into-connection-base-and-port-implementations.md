---
title: ADR-0083 PostgreSQL 어댑터를 연결 기반과 포트의 구현으로 나눈다 — 새 유닛 `adapter.pgconn`, 옮기기만 하고 행동은 바꾸지 않는다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH020
  - GOCOV001
  - TLOCK001
  - backend/internal/pgconn/internal_test.go
  - backend/internal/pgconn/pool_test.go
  - backend/internal/pgconn/tlscheck_test.go
  - backend/internal/pgstore/config_test.go
touches_protected:
  - docs/design-docs/adr/0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md
  - docs/product-specs/requirements-access.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/postgres-operations.md
  - docs/product-specs/requirements-access.md
amends:
  - adr: "0032"
    what: 결정 1의 PostgreSQL 어댑터 한 유닛과 어댑터는 다른 어댑터를 import하지 않는다는 규칙, 결정 2의 lib/pq를 import하는 유닛, 결정 4의 마이그레이션 실행기와 파일이 있는 자리
amends_tests:
  - go:backend/internal/pgstore::TestConfigFillsDefaultsAndRefusesWhatIsWrong
  - go:backend/internal/pgstore::TestConfigNeverPrintsItsDSN
  - go:backend/internal/pgstore::TestConfigDefaultsAndRoleOrder
  - go:backend/internal/pgstore::TestMigrationFilesAreNumberedWithoutGapsAndCarryKnownDirectives
  - go:backend/internal/pgstore::TestMigrationsAreRecordedAndAnAppliedOneIsNeverEdited
  - go:backend/internal/pgstore::TestMigrationWaitsForTheLockWithinItsLimitAndThenTakesTurns
  - go:backend/internal/pgstore::TestTheLimitsOfARoleApplyToItsTransactionsOnly
  - go:backend/internal/pgstore::TestInspectTLSJudgesTheLinkOfAConnectionString
  - go:backend/internal/pgstore::TestInspectTLSReportsTheModeAndHidesTheString
test_change_reason: 연결과 설정, 마이그레이션, 역할의 시간 한도, TLS 확인은 새 유닛 `adapter.pgconn`으로 옮기므로 잠금의 키(패키지 폴더)가 바뀐다. 아홉 테스트 가운데 다섯은 몸체와 쓰는 도우미가 글자까지 그대로라 옛 지문과 새 지문이 같다. 넷은 테스트 도우미가 `Store`에서 `Pool`로 바뀌어(또 하나는 `PollInterval` 단언이 `pgstore`의 새 테스트로 가서) 몸체가 조금 바뀌고, 단언은 하나도 줄지 않는다. 요구사항은 바뀌지 않았고 코드가 있는 자리만 바뀐다.
decision_hash: 1e016c44298d0230
amended_by:
  - adr: "0085"
    what: 결정 3과 결정 7의 공용 부품 목록에 pgschema를 더한다(계획이 뒤의 일에서 지금의 유닛이 된다)
---
# ADR-0083 PostgreSQL 어댑터를 연결 기반과 포트의 구현으로 나눈다

## 맥락

ADR-0080과 ADR-0081이 저장과 질의를 PostgreSQL 하나 위에서 다시 설계했다. 구현은 두 갈래로 나란히 하려 한다. 하나는 운영 데이터와 연결(연결 규칙, 정의와 시크릿의 저장)이고, 다른 하나는 새 KG 저장이다.

지금은 `adapter.pgstore` 한 유닛이 두 가지를 함께 가진다. 연결 기반(풀, 설정, 역할마다 시간 한도를 거는 트랜잭션 도우미, 마이그레이션의 실행기와 파일, TLS 확인, 테스트용 데이터베이스 도우미)과 KG 저장 포트의 구현(과 인증 기록)이다. 이대로 두면 두 갈래가 같은 파일(`store.go`, `config.go`)을 고치게 된다. 정의나 시크릿의 PostgreSQL 어댑터는 연결을 쓰려고 KG 저장(`core.graph`, `core.model`을 아는 코드)을 import해야 하는데, 구조 계약은 어댑터끼리의 import를 막는다(ADR-0032 결정 1).

## 결정

1. **새 유닛 `adapter.pgconn`**(`backend/internal/pgconn`, `may_import: [core.kernel]`)을 만든다. 담는 것은 연결과 풀(`Open`, `Pool`), 설정(`Config`, 역할과 시간 한도), 역할마다 시간 한도를 거는 트랜잭션 도우미(`Pool.Tx`), 드라이버 오류에 이 백엔드의 코드를 붙이는 `DBErr`, 마이그레이션의 실행기와 **마이그레이션 파일 전부**, TLS 확인(`InspectTLS`), 테스트용 데이터베이스 도우미다. 공용 스키마의 DDL은 번호 하나로 이어지는 한 줄이므로 파일은 한 곳에만 둔다. 파일은 `git mv`로 옮기고 **바이트를 바꾸지 않는다**: 체크섬이 이미 적용된 데이터베이스와 맞는다.
2. **`adapter.pgstore`는 KG 저장 포트의 구현과 인증 기록(`Records`)을 가지고 `adapter.pgconn`을 import한다.** KG 저장만의 설정(`PollInterval`)은 `pgstore.Config`에 남는다. `pgstore.Config`는 지금과 같은 필드와 같은 기본값을 가지고, 연결 기반이 읽는 부분만 `pgconn.Config`로 넘긴다.
3. **"어댑터는 서로 import하지 않는다"는 규칙을 고친다.** 서로 다른 기술의 어댑터는 서로 import하지 않는다. PostgreSQL 어댑터들은 포트를 구현하지 않는 공용 부품(`adapter.pgconn`, 뒤에 스키마 계획의 `adapter.pgschema`)만 import할 수 있고, 구조 계약의 `may_import`에 적힌 것만이다. 포트를 구현하는 어댑터끼리(예: `pgstore`가 정의의 PostgreSQL 어댑터를)는 여전히 import하지 않는다.
4. **`github.com/lib/pq`를 import할 수 있는 유닛은 `adapter.pgconn`과 `adapter.pgstore` 둘이다.** `pgstore`는 쓰기 문장에서 `pq.Array`와 `pq.ByteaArray`를 쓰기 때문이다. 뒤에 오는 PostgreSQL 어댑터(정의, 시크릿 등)는 드라이버를 모르고 오류를 `pgconn`의 함수로 가른다.
5. **옮기기만 한다. 행동은 하나도 바꾸지 않는다.** 풀의 수, 오류의 분류, 기본값, 메시지가 그대로다. 그래서 "옮기기만 했다"를 잠긴 테스트의 지문으로 증명할 수 있다(결과 절). 풀을 나누고 오류를 가르는 규칙은 다음 변경이 한다.
6. **다른 유닛이 부르는 이름은 `pgstore`에 남긴다.** 다른 유닛의 코드와 테스트는 한 글자도 바뀌지 않는다.

   | 남기는 이름 | 어떻게 |
   |---|---|
   | `Open`, `Config`, `Store` | `pgstore`의 것. `Open`은 `pgconn.Open`을 부른 뒤 `Store`를 만든다 |
   | `Role`, `Limits`, `RoleApplier`, `RoleAPIRead` | `pgconn`의 별칭(`Config`이 쓴다) |
   | `TLSState`, `InspectTLS` | `pgconn`의 별칭과 넘기는 함수(`cmd.praxium`이 부른다) |
   | `TestDSNEnv`, `TB`, `NewTestDatabase` | `pgconn`의 상수와 별칭과 넘기는 함수 |
   | `NewTestStore`, `NewTestStoreWith` | `pgstore`의 것(`Store`를 연다) |
   | `withDatabase`, `dbErr`, `notFound` | `pgstore` 안의 이름. 앞의 둘은 `pgconn`을 얇게 부르고, `notFound`는 그래프의 오류 코드를 쓰므로 `pgstore`에 있어야 한다 |

7. **구조 계약과 문서.** `architecture.yaml`에 유닛 `adapter.pgconn`을 더하고, `adapter.pgstore`의 `may_import`에 `adapter.pgconn`을 더하고, `third_party`의 `github.com/lib/pq`를 `[adapter.pgconn, adapter.pgstore]`로 하고, 어댑터의 import 규칙의 주석을 결정 3의 말로 바꾼다. `ARCHITECTURE.md`의 유닛 표와 모듈 표, 커버리지 하한(`coverage-floor.json`의 새 키), 요구사항 REQ-472의 테스트 파일 경로를 같은 변경에서 고친다.

## 이유

- 두 갈래가 같은 파일을 고치지 않는다. 연결 기반의 변경(풀 셋, 여러 호스트, 오류의 분류)은 `pgconn`에서, KG 저장의 변경은 `pgstore`에서 한다.
- 다른 PostgreSQL 어댑터가 KG 저장을 import하지 않는다. 정의와 시크릿의 어댑터는 `pgconn`만 import한다.
- 한 번 치르는 값이 작다. 옮겨 가는 잠긴 테스트는 아홉 개이고, 결정 기록 하나의 `amends_tests`에 적는 것으로 끝난다. 이 결정 기록은 새 유닛(구조 계약은 보호 경로) 때문에 어차피 필요하다.
- 마이그레이션 파일을 한 곳에 둔다. 공용 스키마의 DDL이 번호 하나로 이어지므로 두 유닛에 나누면 번호의 주인이 둘이 된다.

## 대안과 기각 사유

- **`pgstore`를 기반으로 두고 다른 PostgreSQL 어댑터가 그것을 import한다.** 잠금의 변화는 없지만 정의와 시크릿의 어댑터가 `core.graph`와 `core.model`을 끌고 오고, 두 갈래가 `store.go`, `config.go`를 함께 고친다. 얻는 것이 없다.
- **그대로 한 유닛으로 둔다(정의와 시크릿의 구현도 `pgstore` 안에).** 잠금의 변화와 새 유닛이 없지만 한 유닛이 연결, KG 저장, 운영 데이터를 다 가지고 커버리지 하한 하나를 두 갈래가 나눠 쓴다.
- **`lib/pq`를 `adapter.pgconn`만 import한다.** `pgstore`가 쓰는 배열 변환(`pq.Array`)을 `pgconn`을 거쳐 넘기게 되어 쓰기 문장마다 얇은 넘김 함수가 생긴다. 이 변경은 옮기기만 하므로 기각한다.

## 결과

- 옮겨 가는 잠긴 테스트 아홉 개의 키가 `go:backend/internal/pgstore::…`에서 `go:backend/internal/pgconn::…`으로 바뀐다. 이 변경은 행동을 바꾸지 않는다. 옛 키는 `amends_tests`에 적었고 새 키는 `pnpm lock-tests`가 더한다. 몸체가 그대로인 다섯은 옛 지문과 새 지문이 같다.

  | 옛 키(`pgstore::`) | 몸체 | 지문 | 바뀐 것과 이유 |
  |---|---|---|---|
  | `TestConfigNeverPrintsItsDSN` | 그대로 | 같다 | 없다 |
  | `TestConfigDefaultsAndRoleOrder` | 그대로 | 같다 | 없다 |
  | `TestMigrationFilesAreNumberedWithoutGapsAndCarryKnownDirectives` | 그대로 | 같다 | 없다 |
  | `TestInspectTLSJudgesTheLinkOfAConnectionString` | 그대로 | 같다 | 없다 |
  | `TestInspectTLSReportsTheModeAndHidesTheString` | 그대로 | 같다 | 없다 |
  | `TestConfigFillsDefaultsAndRefusesWhatIsWrong` | 한 줄 | 다르다 | `PollInterval`이 50ms라는 단언을 뺐다. `PollInterval`은 KG 저장의 설정이라 `pgconn.Config`에 없다. 같은 단언을 `pgstore`의 새 테스트가 한다. 나머지 단언은 그대로다 |
  | `TestMigrationsAreRecordedAndAnAppliedOneIsNeverEdited` | 두 줄 | 다르다 | `NewTestStore(t)`와 `shardOf(t, st)`가 `NewTestPool(t)` 하나가 된다. `Store`를 열지 않고 `Pool`을 연다 |
  | `TestMigrationWaitsForTheLockWithinItsLimitAndThenTakesTurns` | 두 줄 | 다르다 | 같은 도우미 교체(`NewTestPoolWith`). 쓰는 도우미 `holdMigrationLock`의 매개변수 형이 `*shard`에서 `*Pool`로 바뀐다 |
  | `TestTheLimitsOfARoleApplyToItsTransactionsOnly` | 몇 줄 | 다르다 | 같은 도우미 교체. `sh.tx`와 `dbErr`가 이제 공개 이름 `sh.Tx`와 `DBErr`다. 단언은 같다 |

- `pgstore`에 남는 잠긴 테스트 스물두 개와 다른 유닛의 잠긴 테스트는 키도 지문도 바뀌지 않는다(`pnpm check`의 `TLOCK001`이 0건). 남는 테스트 파일의 선언은 하나도 고치지 않았고, 옮겨 간 테스트만 가진 도우미(`files`, `shardOf`, `holdMigrationLock`)는 테스트와 함께 `pgconn`으로 옮겼다. `failRecorder`, `wantCode`, `noErr`는 두 폴더에 모두 있다.
- `adapter.pgconn`은 자기 테스트로 커버리지 하한을 가진다(다른 유닛의 테스트는 세지 않는다). `adapter.pgstore`의 하한은 내려가지 않는다.
- 요구사항 REQ-472의 테스트 파일 경로가 `backend/internal/pgconn/tlscheck_test.go`로 바뀐다.
- 풀을 셋으로 나누기, 오류의 분류, 여러 호스트, 연결 속도 제한, 버전 관문, 스키마 계획(`pgschema`)은 이 결정에 없고 뒤의 변경이 한다.
