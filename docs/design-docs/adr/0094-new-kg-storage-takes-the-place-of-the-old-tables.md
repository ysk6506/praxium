---
title: ADR-0094 새 KG 저장이 옛 테이블의 저장을 대신한다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - backend/internal/pgconn/storage_format_old_test.go
  - backend/internal/pgstore/kgresume_test.go
  - backend/cmd/praxium/storeresume_test.go
  - backend/internal/pgcheck/read_test.go
touches_protected:
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/design-docs/adr/0087-schema-executor-and-shared-tables.md
  - docs/design-docs/adr/0089-new-kg-storage-the-kg-stands-on-its-own-schema.md
  - docs/product-specs/requirements-model-storage.md
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/storage-shape.md
  - docs/product-specs/requirements-model-storage.md
amends:
  - adr: "0085"
    what: 결정 15의 "마이그레이션이 KG에 딸린 공용 테이블의 행도 비운다"가 옛 KG가 든 DB를 거절하는 것으로 바뀐다는 것(옛 테이블이 비었을 때만 지운다)
  - adr: "0087"
    what: 결정 1의 "Open의 설정에 모델 제공자를 다는 것"을 이 변경이 한다는 것과 결정 3의 저장 형식 번호가 2가 된다는 것, 옛 형식의 DB를 거절하는 관문의 더함
  - adr: "0089"
    what: 결과의 "제품의 길은 아직 옛 구현을 쓴다"와 `OpenV2`, `StoreV2`, `NewTestStoreV2`의 이름(제품의 이름은 `Open`, `Store`, `NewTestStore`가 된다)
amends_tests:
  - go:backend/internal/pgconn::TestABuildThatKnowsAnOlderStorageFormatRefusesToStartAndSaysWhy
  - go:backend/internal/pgstore::TestADamagedSchemaFailsEveryOperationInsteadOfAnsweringWrongly
  - go:backend/internal/pgstore::TestAnApplyWhoseCommitGotNoAnswerIsCheckedAgainstTheHeadAndCanBeRepeated
test_change_reason: 옛 테이블이 지워지고 저장 형식 번호가 2가 되어, 옛 표 이름과 옛 번호를 단언하던 세 테스트가 새 표와 새 번호로 같은 것을 확인한다. 단언은 줄지 않았다. 표 이름의 대응과 옛 값, 새 값은 아래 표에 있다.
decision_hash: c24e2084f70e35e8
---
# ADR-0094 새 KG 저장이 옛 테이블의 저장을 대신한다

## 맥락

ADR-0089가 새 KG 저장(KG마다의 저장 스키마 위의 `graph.Store` 구현)을 옛 구현 옆에 더하고 적합성 36개를 통과시켰다. 제품의 길(`pgstore.Open`)은 아직 옛 테이블을 썼다. ADR-0085 결정 15는 새 구현이 준비되면 한 변경에서 `Open`과 `NewTestStore`가 새 구현을 주게 하고 옛 테이블의 코드와 테이블을 함께 삭제로 했다. 이 변경이 그 한 변경이다. 옛 데이터를 새 테이블로 옮기지 않기로 했으므로 두 구현이 함께 도는 기간은 뜻이 없다.

## 결정

1. **저장은 하나다.** `pgstore.Open`은 새 저장(`Store`)을 돌려주고 `NewTestStore`/`NewTestStoreWith`도 같다. 모델은 `Config.Models`로 받고(ADR-0087 결정 1이 남긴 것), 없으면 표준 모델(통계 객체 포함)을 쓴다. 호출자의 시계로 리스를 판단하는 것은 `Config.LeaseClock`이고 서버는 DB의 시계를 쓴다. `NewTestStore`는 호출자의 시계를 쓴다(적합성과 다른 테스트가 시계를 옮긴다). `NewTestStoreWith(tb, clock)`로 DB의 시계를 고른다.
2. **옛 저장의 코드를 지운다.** 옛 `Store`와 그 쓰기, 읽기, 피드, 리스, 카탈로그 파일을 지운다. 새 저장도 쓰는 부품(행 변환, 코덱, 쓰기 도우미, 머리와 리스의 행)은 `common.go`와 `rows.go`로 옮겼다. 옛 이름 `StoreV2`, `OpenV2`, `V2Options`, `NewTestStoreV2(With)`는 `v2names.go`의 별칭으로 남겼다. 이 이름을 부르는 잠긴 테스트가 33개라서 이름을 떼면 그 모두가 `amends_tests`에 든다. 그 테스트를 어차피 바꾸는 변경이 파일을 지운다.
3. **옛 테이블은 새 마이그레이션 `0005`가 지운다.** 17개 테이블(`kg`, `kg_head`, `kg_apply_lease`, `generation_event`, `object_live`, `object_hist`, `link_live`, `link_hist`, `attr_volatile`, `light_change`, `object_identifier`, `identity_event`, `ingest_order`, `change_batch`, `change_entry`, `kg_version`, `kg_version_hash_part`)과 그 인덱스를 지우고 저장 형식 번호를 2로 올린다(`pgconn.StorageFormat`). 이미 번호가 붙은 마이그레이션은 고치지 않았다. 인증 기록(`identity_record`)과 새 공용 테이블은 건드리지 않는다.
4. **옛 KG가 든 DB는 거절한다.** 알파 릴리스가 만든 DB의 옛 `kg` 테이블에 KG가 있으면 마이그레이션을 돌리기 **전에** `pgconn`이 거절한다(`CodeStorageFormatOld`, `Codes()`의 밖). 옮기는 길이 없고, 마이그레이션이 데이터를 말없이 지우게 두지 않기 위해서다. 이것이 ADR-0085 결정 15를 바꾸는 점이다: 그 결정은 마이그레이션이 KG에 딸린 공용 테이블의 행도 비운다고 했지만, 알파 사용자가 새 판을 처음 띄우는 것만으로 KG를 잃는 것보다 알리고 멈추는 쪽이 낫다. KG가 없는 옛 테이블은 `0005`가 지운다. `0005`의 맨 앞에도 같은 거절이 있어서 관문을 모르는 실행기가 이 단계를 돌려도 행을 지우지 않는다. 거절하는 글은 "이 DB는 저장 형식 1(알파 릴리스의 테이블)에 KG를 가지고 있다. 새 저장 형식으로 옮기는 길은 없다. 새 DB에 소스를 다시 적재하라"이다. 새 빌드가 만든 DB(번호 2)를 알파 실행 파일이 열면 옛 테이블이 없어서 모든 연산이 오류가 난다(알파 실행 파일에는 관문이 없다).
5. **`ResumeWork`는 두 곳에 연결한다.** (가) **서버가 시작할 때**: `cmd/praxium`이 PostgreSQL 저장을 연 직후(`-store postgres`의 길. 인증 명령은 해당 없음) 모든 KG의 머리를 보고 끊긴 버리기나 복원을 이어서 끝낸다(`Store.ResumeAllWork`). 어떤 KG의 일을 끝내지 못해도 서버는 뜬다. 경고 로그를 남기고 그 KG는 머리가 말하는 상태(복원 중이면 닫힌 채)로 남는다. 서버가 읽는 KG가 닫힌 채로 시작하면 나중에 고쳐도 이미 늦기 때문에 시작할 때 한 번 하고, 실패는 가용성을 위해 경고로 둔다. (나) **리스를 줄 때**: `AcquireLease`가 머리에 끊긴 일이 있으면 그 일을 먼저 끝내고 리스를 준다(최대 3번 다시 본다). 쓰려는 쪽이 리스를 잡는 길이라서, 서버가 도는 동안 끊긴 일이 생겨도(다른 서버가 죽었다) 다음 쓰기가 온전한 KG를 만난다. 읽기와 `ApplyChunk`에는 걸지 않는다: 읽기마다 머리를 한 번 더 읽는 비용이 들고, 복원 중인 KG를 읽는 쪽은 `CodeKGRestoring`을 받으며 곧 열린다. 두 워커이 같은 일을 동시에 이어도 번호마다 머리 행을 잠그고 다시 읽으므로 안전하다.
6. **`adapter.pgcheck`에 질의 엔진의 읽기 비교를 더한다.** 지금의 질의 엔진이 쓰는 읽기(`find`의 행을 타입마다 한 쪽씩, `neighbors`의 세 방향, `get`의 속성·식별자·링크 수)가 새 저장과 메모리 저장에서 같은 답을 내고, 과거 변경 번호(1, 중간, 끝 바로 앞)에서도 같다. 옛 저장과 비교하던 테스트은 없었고 측정(`measure_test.go`)에서 옛 저장을 뺐다.

## 이유

- 두 저장을 함께 두면 같은 이름의 `Store`가 둘이고 제품이 어느 쪽을 쓰는지 읽기 어렵다. 옛 데이터는 옮기지 않으므로 함께 둘 값이 없다.
- 옛 KG를 말없이 지우는 마이그레이션은 되돌릴 수 없다. 관문이 먼저 알리면 사용자는 DB를 그대로 둔 채 새 DB로 가거나, 옛 실행 파일로 돌아갈 수 있다.
- 끊긴 일을 이어서 끝내는 것은 서버가 하나 죽고 다른 서버가 뜨는 HA의 기본이다. 시작할 때와 리스를 줄 때 두 곳이면 서버가 도는 동안 생긴 끊김과 재시작 직후의 끊김을 모두 덮는다.

## 대안과 기각 사유

- **마이그레이션이 옛 KG를 지운다**(ADR-0085 결정 15의 원래 모양). 사용자의 KG를 알리지 않고 지운다.
- **서버를 띄울 때 끊긴 일을 이어 끝내지 못하면 시작하지 않는다.** KG 하나 때문에 모든 KG를 못 읽는다.
- **읽기 길에서도 끊긴 일을 이어서 끝낸다.** 읽기마다 머리를 한 번 더 읽는다. 읽기가 쓰기를 하게 된다.
- **`V2`가 든 이름을 모두 뗀다.** 잠긴 테스트 33개가 바뀐다.

## 결과

- 알파 사용자는 PostgreSQL에 둔 KG를 새 DB에 다시 수집한다. 릴리스 노트에 다음을 적는다: "이 판에서 PostgreSQL의 KG 저장 형식이 바뀌었다(저장 형식 2). 이전 판(알파)이 만든 DB에 KG가 있으면 이 판은 시작하지 않고 이유를 알린다. 옮기는 길은 없다. 새 DB를 만들어 소스를 다시 적재하라. 이 판이 만든 DB를 이전 판의 실행 파일로 열면 모든 연산이 오류가 난다."
- 바뀌는 잠긴 테스트 세 건과 대응(건마다 옛 단언과 새 단언):

| 테스트 | 옛 | 새 |
|---|---|---|
| `TestABuildThatKnowsAnOlderStorageFormat…` | 형식 번호 1, 큰 번호의 거절 글이 `storage format 2` | 형식 번호 2, 거절 글이 `storage format 3` |
| `TestADamagedSchemaFailsEveryOperation…` | 옛 공용 표 이름(`object_live`, `kg_head` 등)을 바꿔 치고 모든 연산이 오류 | 새 저장 스키마의 표(`object`, `t_<번호>`, `link`, `head` 등)와 `kg_catalog`로 같은 단언, 버리기와 복원은 반쪽 적용이 있는 KG에서 표마다, 표를 되돌리면 `ResumeWork`가 일을 끝냄 |
| `TestAnApplyWhoseCommitGotNoAnswer…` | 도우미가 옛 `kg_head`를 보고 연결을 끊음(본문은 그대로) | 도우미가 새 `head`를 보고 끊음 |

- 이 변경의 새 테스트는 11건이다(옛 형식 DB의 거절, `0005`의 삭제와 자체 거절, 서버 시작과 리스에서의 이어 끝내기, 질의 엔진의 읽기 비교).
- 상한 옵션(ADR-0085 결정 11)과 `v2names.go`의 별칭은 뒤의 변경에 남는다.
