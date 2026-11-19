---
title: ADR-0087 스키마 실행기와 공용 테이블
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - backend/internal/pgconn/schema_state_test.go
  - backend/internal/pgstore/schema_exec_test.go
touches_protected:
  - docs/product-specs/requirements-model-storage.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/storage-shape.md
  - docs/design-docs/postgres-operations.md
  - docs/product-specs/requirements-model-storage.md
amends: []
decision_hash: 5ca2c6c45b5cec29
amended_by:
  - adr: "0089"
    what: 스키마 상태(kg_schema_state)의 자리는 그대로 공용이라는 것의 확인(바뀌는 것은 없다)
---
# ADR-0087 스키마 실행기와 공용 테이블

## 맥락

ADR-0085가 KG 하나의 저장 스키마를 계획하는 순수한 유닛 `adapter.pgschema`를 들였고, 그 계획을 DB에 실행하는 일과 `pgstore`가 그 유닛을 import하는 일은 뒤의 변경에 남겼다. 이 변경이 실행기와 그것이 쓰는 공용 테이블을 짓는다. 지금의 KG 저장(옛 테이블 위의 포트 구현)은 그대로 돌고 새 것은 옆에 더한다.

## 결정

1. **`adapter.pgstore`의 `may_import`에 `adapter.pgschema`를 더한다.** ADR-0083과 ADR-0085 결정 1이 정한 것이다. 실행기는 `pgstore`의 새 파일들(`schema_*.go`)에 둔다. 모델은 호출자가 레지스트리로 주고, `Open`의 설정에 모델 제공자를 다는 것과 `CreateKG`와 `DeleteKG`에 거는 것은 새 저장의 변경이 한다.
2. **공용 마이그레이션 `0003`**이 시각의 불변 함수 `public.praxium_ts`(`pgschema.TimeFunc`가 정한 이름과 인자), `kg_no`의 시퀀스 `kg_no_seq`, KG마다 한 행인 `kg_schema_state`(KG, `kg_no`, 생성기 버전, 모델·번호·검색 집합의 해시, 매니페스트 jsonb, 상태, 적용한 때), 한 행인 `storage_format`을 더한다. 옛 테이블은 건드리지 않는다.
3. **저장 형식 번호의 관문**은 버전 관문과 같은 자리(`pgconn`의 시작)에서 마이그레이션 뒤에 돈다. DB의 번호가 이 빌드가 아는 `StorageFormat`보다 크면 이유를 알리고 시작하지 않는다. 오류 코드 `pgconn_storage_format_too_new`는 `Codes()`의 목록 밖에 둔다(그 목록의 개수를 기존 테스트가 고정한다).
4. **트랜잭션 밖의 문장은 `Pool.ExecOutsideTx` 하나로만 돈다.** `CREATE INDEX CONCURRENTLY`가 트랜잭션에 들어가지 않기 때문이다. 쓰기 풀의 연결 하나에서 돌고, 세션에 아무것도 남기지 않으며(시간 한도는 문맥의 시한), 되풀이해도 같게 문장을 쓴다(`IF NOT EXISTS`, `IF EXISTS`). 세션 상태를 훑는 검사는 이 한 파일(`online_exec.go`)을 `testdb.go`처럼 `Exec`의 이름 검사에서 뺀다. 문자열 검사는 그대로 걸린다. 다른 파일에서는 여전히 트랜잭션이 아닌 곳의 `Exec`가 금지다.
5. **실행기의 규칙.** 만들기는 한 트랜잭션(쓰기 풀, 마이그레이션 역할의 시한, KG마다의 `pg_advisory_xact_lock`, 잠금 대기가 끝나면 몇 번 다시 시도)에서 `kg_no`를 받고 계획의 문장을 돌리고 행을 마지막에 쓴다. 같은 모델로 다시 부르면 아무것도 바꾸지 않는다. 맞추기는 `Diff`의 트랜잭션 안의 단계를 한 트랜잭션에, 트랜잭션 밖의 단계를 하나씩 돌린다. 인덱스를 만들기 전에 무효 인덱스를 지우고, 끝난 뒤 스키마의 인덱스와 매니페스트를 견줘 모자란 것을 만들고 모르는 검색 인덱스를 지운다. 더하는 변경이 아니면 아무것도 바꾸지 않고 거절한다. 삭제는 `DROP SCHEMA ... CASCADE`와 행 삭제를 한 트랜잭션에서 한다. `kg_no`는 다시 쓰지 않는다.
6. **행의 상태에는 `building`이 있다.** 매니페스트의 상태(`ok`, `index_pending`, `stats_pending`)에 더해, 인덱스를 만드는 실행이 있는 동안 행이 `building`과 시작한 때를 가진다. 같은 KG를 맞추려는 다른 실행은 충돌로 거절되고, 30분 동안 아무 말이 없으면 이어받는다. 실행이 중간에 죽어도 `building`이 남을 뿐 데이터는 상하지 않는다(무효 인덱스는 다음 실행이 지운다). 통계가 아직 없는 KG(`stats_pending`)는 `FinishStorageStats`가 만든 뒤에만 맞춘다.
7. **어긋남의 판단**은 저장된 생성기 버전과 세 해시를 서버의 모델과 견주는 순수한 함수 `CheckSchemaMatch`와 이름으로 읽는 `CheckStorageSchema`다. 코드는 `pgstore_schema_mismatch`다. 적용과 질의의 길에 거는 것은 새 저장의 변경이 한다.

## 이유

- 실행기를 `pgstore`에 두면 계획은 순수한 채로 있고 DB의 일(잠금, 재시도, 무효 인덱스)만 실행기가 맡는다(ADR-0085).
- 인덱스를 만드는 동안 트랜잭션을 열어 잠금을 쥐면 유휴 시한에 걸리고 사이의 일을 막는다. 행의 `building` 표시는 세션 잠금 없이 두 실행이 서로의 반쯤 만든 인덱스를 지우지 못하게 한다.
- 스키마와 인덱스의 실제 상태를 매니페스트와 견주어 되돌리는 것이, 어느 단계에서 죽었는지를 따로 적는 것보다 단순하고 어디서 죽어도 같은 끝에 닿는다.

## 대안과 기각 사유

- **인덱스를 만드는 동안 열어 둔 트랜잭션이 잠금을 쥔다.** 유휴 시한이 있고 긴 트랜잭션은 청소를 막는다.
- **세션 어드바이저리 잠금으로 인덱스 만들기를 직렬화한다.** 연결 풀과 맞지 않아 금지다(`postgres-operations.md` 6번).
- **`CREATE INDEX CONCURRENTLY`를 마이그레이션 파일에 둔다.** 마이그레이션은 파일마다 한 트랜잭션이다. 그리고 인덱스는 모델에 따라 바뀌어 번호 파일에 못 둔다.

## 결과

- `pgstore`는 `CreateStorageSchema`, `AdjustStorageSchema`, `FinishStorageStats`, `DropStorageSchema`, `StorageSchema`, `CheckStorageSchema`, `CheckSchemaMatch`를 내보낸다. 아직 제품의 어떤 길에서도 부르지 않는다.
- 표준 모델의 KG 하나(테이블 215개, 문장 833개)는 한 트랜잭션에서 0.5~1.3초에 만들어진다(측정은 변경의 보고에 있다). 한 트랜잭션이 잡는 잠금이 많으므로 `max_locks_per_transaction`을 배포 때 확인한다.
- REQ-565와 REQ-632의 예정 표시를 뗀다. REQ-560~562는 뷰와 적용·질의의 거절이 남아 있어 예정으로 둔다.
