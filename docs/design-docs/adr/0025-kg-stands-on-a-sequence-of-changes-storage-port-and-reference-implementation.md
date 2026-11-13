---
title: ADR-0025 KG를 변경 시퀀스 위에 구성한다 — 스토리지 포트와 기준 구현
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH013
  - GOARCH052
  - GOTEST002
  - GOCOV001
  - TRACE006
  - backend/internal/memstore/memstore_test.go
  - backend/internal/graphcheck/graphcheck_test.go
  - backend/internal/graph/apply_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-storage.md
  - docs/design-docs/kg-storage-apply.md
  - docs/exec-plans/active/plan.md
  - docs/product-specs/requirements.md
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0009-kg-storage-and-versions.md
amends:
  - adr: "0009"
    what: 링크의 내용 해시에 등급과 근거의 종류가 들고, 반쯤 든 배치는 되감아 포기한다
decision_hash: 17e1b4cb4439675c
amended_by:
  - adr: "0028"
    what: 적합성 테스트 단위가 기준 구현을 import하는 범위
---
# ADR-0025 KG를 변경 시퀀스 위에 구성한다: 스토리지 포트와 기준 구현

## 맥락

ADR-0009와 두 설계 문서(`kg-storage.md`, `kg-storage-apply.md`)는 KG의 테이블, ID와 해시, 큰 적용의 청크, 리스, 세대, 확정을 정했다. 코드는 없었다. 다음 단계(사전 정의 질의, 경로, 개발용 적재, PostgreSQL)가 모두 이 위에 올라가므로 먼저 **핵심과 스토리지의 경계**를 코드로 세우고 어느 스토리지든 같은 의미를 내게 하는 테스트를 둔다. 문서에 비어 있거나 어긋난 부분이 있어서 가장 단순한 해석을 골라 여기에 적는다.

## 결정

1. **단위 3개.** `core.graph`(`backend/internal/graph`, `core.kernel`과 `core.model`만 import한다), `adapter.memstore`(`backend/internal/memstore`, `core.kernel`과 `core.graph`를 import한다), `adapter.graphcheck`(`backend/internal/graphcheck`, 적합성 테스트. `core.kernel`, `core.model`, `core.graph`를 import하고 테스트에서만 `adapter.memstore`를 실행한다). PostgreSQL 어댑터와 드라이버는 다음 스토리지 변경에서 제공한다. 적합성 테스트는 `testing`을 import하지 못하므로 작은 `TB` 인터페이스와 `Cases()`를 제공하고, 어댑터의 테스트가 `t.Run`으로 실행한다. 지금 사례는 31개다. 적합성 테스트를 `core.graph` 안에 두려 했으나 커버리지가 단위마다 그 단위 자신의 테스트로 측정되고 하한이 90 이상이어야 해서(GOCOV), 테스트의 문장이 `core.graph`의 측정값을 끌어내렸다. 그래서 별도 단위로 두었다.
2. **ID는 생성하지 않고 계산한다.** 객체 ID는 `("praxium.object.v1", 타입의 불변 ID, 식별 키 구성요소들)`의 정규 해시 앞 16바이트(`obj_` + 32 16진), 링크 ID는 `("praxium.link.v1", 링크 타입 ID, 출발, 도착, discriminator)`(`lnk_`), 버전 ID는 `("praxium.kgversion.v1", 앞 버전, 정의 세트 해시, 입력 체인, 의미 버전, 해시 규칙 버전)`(`kv_`)이다. 구성요소는 속성(참조, 정규화한 값), 문맥(객체 하나), 집합(객체 ID를 정렬), 식별 방식(방식 ID와 값)이다. 같은 입력이면 어떤 생성기 없이도 같다. `kg_id`, 적용 ID, 리스 소유자는 주입받은 `IDGenerator`가 발급하고 해시에 포함되지 않는다.
3. **행은 번호로, 해시는 ID로**(REQ-233). 행은 `type_no`, `link_type_no`, `attr_no`를 갖고 내용 해시와 부분합은 변하지 않는 ID만 쓴다. 이름과 번호를 바꿔도 모든 해시가 그대로다(테스트가 강제한다). **내용 해시의 고정 목록에 링크의 근거 등급과 근거 종류를 추가한다**(문서에 빠져 있었다. 구현 전이라 해시 규칙의 버전은 1 그대로다). 목록은 모두 정렬해 해시한다(등록부에 순서를 선언하는 필드가 아직 없다). `confidential` 값은 `KeyProvider` 포트의 키를 쓰는 키 기반 해시로 포함된다. 이 단위는 결정적인 테스트용 제공자만 제공한다.
4. **적용기가 결정하고 스토리지는 쓰기만 한다.** 적용기는 현재 상태와 비교해 차이, 행 해시, 새 헤드(그래프 해시와 타입별 부분합, 입력 체인)를 만들고, 스토리지는 청크 하나를 원자적으로 쓴다. 청크는 배치의 후보(추가, 수정, 폐기)를 `(종류, ID)` 순으로 정렬해 한도(기본 1만, 가정)만큼 자른다. 자르기는 상태에 의존하지 않아 다시 해도 같다. 청크마다 변경 번호 하나, 차이가 없는 청크는 쓰지 않는다. `volatile` 값, 식별자, 식별 이벤트, 순서 이벤트는 **마지막 청크**에 포함된다. `readable_through`는 마지막 청크에서만 `applied_seq`로 오르고 초기 적재라면 같은 쓰기에서 `building`이 `serving`이 된다. 차이가 없는 배치는 변경 번호를 쓰지 않지만 순서 이벤트는 남긴다.
5. **포기는 롤백이다**(`kg-storage-apply.md` 10절의 열린 쟁점을 해소한다). 아직 읽을 수 없는 청크는 아무도 볼 수 없었으므로 `readable_through` 뒤의 행을 삭제하고 그 청크가 닫은 행을 다시 열고 헤드를 시작 시점으로 되돌린다. 변경 번호는 다시 사용된다. 리버트(ADR-0014)와 다르다: 새 변경도 새 버전도 없다. 적용 ID가 헤드에 남아 있으면 같은 ID의 배치가 이어서 처리하고, 끝난 적용의 재시도는 `LastApplyID`로 한 번만 반영된다.
6. **순서와 확정.** 순서 이벤트는 고정 목록 8종(`source_change`, `clock_event`, `seal`, `held_release`, `held_discard`, `guard_confirm`, `raw_exclude`, `definition_switch`)이고 `ingest_seq`를 받아 `h(n) = H(h(n-1), ingest_seq, 종류, 참조, 배치 내용 해시)`의 입력 체인을 만든다. 확정은 체인을 0으로 되돌리고 `seal` 이벤트가 된다. 확정은 `applied_seq`와 `readable_through`가 같고 열린 적용이 없을 때만 할 수 있고, 그래프 해시가 앞 버전과 같으면 새 버전이 없다(빈 KG의 첫 확정도 새 버전을 만들지 않는다). 선택적으로 확정 전에 전체를 다시 계산해 비교한다(`Verify`).
7. **리스.** `Acquire`는 `attempt`를 올린다. 다른 소유자의 유효한 리스가 있거나, 헤드가 적용한 의미 버전(정수)보다 낮으면 거절한다. 모든 쓰기는 기대하는 `applied_seq`, 세대, 리스(소유자, `attempt`, 만료 시각)를 포함해 전달하고 스토리지가 검증한다. 의미 버전은 마지막 청크에서 헤드에 반영된다.
8. **세대.** `Restore`가 세대를 올리고 이벤트를 남기며 이후의 행, 읽을 수 있는 번호, 버전을 폐기한다. 헤드는 적용기가 남은 행으로 다시 계산한다(`RecomputeHash`). 이전 세대의 읽기 지점, 이미 확보한 스냅샷, 쓰기는 `graph_generation_changed`다.
9. **`volatile`.** 행과 해시에 포함되지 않고 최신 값 하나만 둔다. 바뀐 값만 `light_change`의 레코드(새 값의 짧은 해시)가 되고, 같은 값은 변경이 아니다. 읽기 지점은 `ReadLatest`(=`readable_through`, 값 있음), `ReadApplied`(적용기의 것, 값 있음), `ReadSeq`, `ReadVersion`(값 없음, `graph_volatile_omitted`와 `volatile_as_of: omitted`)이다. `change_seq`를 시간 여행 입력으로 받는다(추가).
10. **포트의 4가지 역할.** `Catalog`(KG: 생성, `main`을 포함한 조회, 복구, 세대 목록), `Writer`(리스 획득, 연장, 해제와 `ApplyChunk`, `AbandonApply`, `Seal`), `Reader`(`Head`, `Lease`, `Snapshot(ReadPoint)`, `Versions`), `Feed`(`AppliedChanges`, `LightChanges`, `OrderEvents`, `WaitReadable(min_seq, wait_timeout)`). 쓰기 3개는 헤드 행을 잠그고 기대 헤드, 세대, 리스를 확인한 뒤 모든 행과 헤드를 한 트랜잭션으로 쓴다. 읽기 측은 쓰기가 끝나기 전의 청크를 볼 수 없다. 행의 스키마는 `kg-storage.md` 3절의 테이블을 따른다(부분합만 `type_id`로 바뀌었다).
11. **메모리 어댑터.** 한 뮤텍스 아래 구간(interval)을 가진 행의 체인을 둔 완전하고 단순한 구현이다. 적합성 테스트를 모두 통과하고 `-race`를 통과하며, 테스트와 개발 모드의 스토리지이고 PostgreSQL 어댑터의 기준이다. 재시작하면 데이터를 잃으므로 운영에서 쓰지 않는다(운영 모드의 조립이 이것을 거절하도록 하는 것은 조립을 만드는 변경이 한다).
12. **문서가 비어 있거나 어긋나서 고른 것.** 행의 `name`은 별개의 필드이고 `name` 속성과 겹쳐도 둘 다 해시에 포함된다. 업서트는 버전 속성을 통째로 바꾼다. 해시에 포함되지 않는 필드(출처, 시각)만 달라진 업서트는 변경이 아니다. 속성 값은 문자열, `int64`, 불리언, 목록, 맵이고 수는 십진 문자열이다. 링크의 끝은 배치 뒤에도 존재해야 하고, 링크가 남은 객체는 폐기할 수 없다(링크를 같은 배치에서 폐기해야 한다). 카디널리티는 `:1` 측의 개수를 본다. 필수 속성, 매개 값 제약, 식별 키 값의 정규화가 맞는지는 수신 측(`core.ingest`)의 일이라 확인하지 않는다. 폐기된 객체의 `volatile` 값은 남지만 읽히지 않는다. 순서 이벤트의 `ChangeSeq`는 그때의 `applied_seq`다. 부분합은 `type_id`로 센다.

## 이유

- 같은 테스트로 어댑터의 동작을 제한하면 PostgreSQL이 늦게 와도 상위 단위(질의, 경로)를 지금 만들 수 있고, 어댑터가 의미를 바꾸지 못한다.
- 결정을 코어에 두면(차이, 해시, 청크 자르기) 어댑터가 둘이어도 ID, 번호, 해시, 버전이 같다(REQ-131의 기반).
- 포기를 롤백으로 정하면 큰 적용의 모든 실패에 정해진 복구 절차가 생긴다.

## 대안과 버린 이유

- 적합성 테스트를 `core.graph` 안에 두기: 포트와 함께 바뀌어 낡지 않지만 테스트의 문장이 코어의 커버리지를 끌어내려 하한 90을 지키지 못한다. 그래서 따로 두고, 포트가 바뀔 때 같은 변경에서 수정한다.
- 스토리지가 차이를 계산하기(SQL로): 어댑터마다 의미가 갈라진다(ADR-0006의 "의미의 구현은 하나").
- 포기를 앞으로 나아가는 변경으로 하기: 아무도 보지 못한 청크에 새 변경과 번호를 쓰게 되어 번호가 낭비되고 버전이 늘어난다.
- 번호를 해시에 넣기: 번호를 바꾸는 마이그레이션에서 모든 해시가 바뀐다.

## 한계

- 적합성 테스트가 확인하는 범위는 이 구현이 아는 만큼이다. 어댑터가 새로 알게 되는 것(잠금 대기, 커넥션)은 어댑터의 테스트가 맡는다.
- 메모리 어댑터는 스캔과 링크 조회가 정렬 목록 위에서 이루어져 큰 KG에서 느리다. 규모는 PostgreSQL 어댑터에서 측정한다.
- 메모리 어댑터의 테스트는 적합성 테스트를 쓰지 않고 적용기로 직접 실행한다. `adapter.graphcheck`의 테스트가 메모리 어댑터를 실행하므로 반대로 import하면 단위의 순환이 생기기 때문이다.
- 사전 정의 질의, 경로, 계약, 명령줄, 개발용 적재, PostgreSQL은 이 변경에 없다.

## 요구사항 변경 내역

- **예정을 해제했다**: REQ-132, REQ-135.
- **분리했다**: REQ-131에서 REQ-246(변경 시퀀스부터의 결정성), REQ-232에서 REQ-247(경량 변경 레코드와 과거 읽기의 생략), REQ-237에서 REQ-248(세대), REQ-222에서 REQ-249(분할 적용과 대기). 남은 REQ-131, REQ-222, REQ-232, REQ-237은 예정으로 있다.
- **테스트를 연결했다**: REQ-233.
