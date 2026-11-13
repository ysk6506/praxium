---
title: ADR-0025 KG가 변경 시퀀스로 선다 — 저장소 포트와 기준 구현
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
decision_hash: a90e7f3846abeae3
amended_by:
  - adr: "0028"
    what: 적합성 테스트 단위가 기준 구현을 import하는 범위
---
# ADR-0025 KG가 변경 시퀀스로 선다: 저장소 포트와 기준 구현

## 맥락

ADR-0009와 두 설계 문서(`kg-storage.md`, `kg-storage-apply.md`)는 KG의 표, ID와 해시, 큰 적용의 쪽, 리스, 세대, 확정을 정했다. 코드는 없었다. 다음 걸음(사전 정의 질의, 길, 개발용 적재, PostgreSQL)이 모두 이 위에 서므로 먼저 **핵심과 저장소의 경계**를 코드로 세우고 어느 저장소든 같은 뜻을 내게 하는 테스트를 둔다. 문서에 빈 곳과 어긋난 곳이 있어서 단순한 읽기를 골라 여기에 적는다.

## 결정

1. **단위 셋.** `core.graph`(`backend/internal/graph`, `core.kernel`과 `core.model`만 읽는다), `adapter.memstore`(`backend/internal/memstore`, `core.kernel`과 `core.graph`를 읽는다), `adapter.graphcheck`(`backend/internal/graphcheck`, 적합성 테스트. `core.kernel`, `core.model`, `core.graph`를 읽고 테스트에서만 `adapter.memstore`를 돌린다). PostgreSQL 어댑터와 드라이버는 다음 저장소 변경이 낸다. 적합성 테스트는 `testing`을 import하지 못하므로 작은 `TB` 인터페이스와 `Cases()`를 내고, 어댑터의 테스트가 `t.Run`으로 돌린다. 지금 사례는 31개다. 적합성 테스트를 `core.graph` 안에 두려 했으나 커버리지가 단위마다 그 단위 자신의 테스트로 재어지고 하한이 90 이상이어야 해서(GOCOV), 테스트의 문장이 `core.graph`의 측정을 끌어내렸다. 그래서 단위로 따로 두었다.
2. **ID는 만들어 내지 않고 계산한다.** 객체 ID는 `("praxium.object.v1", 타입의 바뀌지 않는 ID, 식별 키 구성요소들)`의 정규 해시 앞 16바이트(`obj_` + 32 16진), 링크 ID는 `("praxium.link.v1", 링크 타입 ID, 출발, 도착, discriminator)`(`lnk_`), 버전 ID는 `("praxium.kgversion.v1", 앞 버전, 정의 묶음 해시, 입력 사슬, 의미 버전, 해시 규칙 버전)`(`kv_`)이다. 구성요소는 속성(참조, 다듬은 값), 문맥(객체 하나), 집합(객체 ID를 정렬), 식별 방식(방식 ID와 값)이다. 같은 입력이면 어떤 생성기 없이도 같다. `kg_id`, 적용 ID, 리스 주인은 주입받은 `IDGenerator`가 내고 해시에 들지 않는다.
3. **행은 번호로, 해시는 ID로**(REQ-233). 행은 `type_no`, `link_type_no`, `attr_no`를 갖고 내용 해시와 부분합은 바뀌지 않는 ID만 쓴다. 이름과 번호를 바꿔도 모든 해시가 그대로다(테스트가 건다). **내용 해시의 고정 목록에 링크의 근거 등급과 근거의 종류를 더한다**(문서에 빠져 있었다. 구현 전이라 해시 규칙의 버전은 1 그대로다). 목록은 모두 정렬해 해시한다(등록부에 순서를 선언하는 칸이 아직 없다). `confidential` 값은 `KeyProvider` 포트의 키 단 해시로 든다. 이 단위는 결정적인 테스트용 제공자만 낸다.
4. **적용기가 정하고 저장소는 쓰기만 한다.** 적용기는 현재 상태와 견줘 차이, 행 해시, 새 머리(그래프 해시와 타입별 부분합, 입력 사슬)를 만들고, 저장소는 쪽 하나를 원자적으로 쓴다. 쪽은 배치의 후보(더하기, 고치기, 물리기)를 `(종류, ID)` 순으로 세워 한도(기본 1만, 가정)만큼 자른다. 자르기는 상태에 기대지 않아 다시 해도 같다. 쪽마다 변경 번호 하나, 차이가 없는 쪽은 쓰지 않는다. `volatile` 값, 식별자, 식별 사건, 차례의 사건은 **마지막 쪽**에 든다. `readable_through`는 마지막 쪽에서만 `applied_seq`로 오르고 초기 적재라면 같은 쓰기에서 `building`이 `serving`이 된다. 차이가 없는 배치는 변경 번호를 쓰지 않지만 차례의 사건은 남긴다.
5. **포기는 되감기다**(`kg-storage-apply.md` 10절의 열린 쟁점을 닫는다). 아직 읽을 수 없는 쪽은 아무도 못 봤으므로 `readable_through` 뒤의 행을 지우고 그 쪽이 닫은 행을 다시 열고 머리를 시작 때로 돌린다. 변경 번호는 다시 쓰인다. 리버트(ADR-0014)와 다르다: 새 변경도 새 버전도 없다. 적용 ID가 머리에 남아 있으면 같은 ID의 배치가 이어받고, 끝난 적용의 재시도는 `LastApplyID`로 한 번만 반영된다.
6. **차례와 확정.** 차례의 사건은 닫힌 여덟 가지(`source_change`, `clock_event`, `seal`, `held_release`, `held_discard`, `guard_confirm`, `raw_exclude`, `definition_switch`)이고 `ingest_seq`를 받아 `h(n) = H(h(n-1), ingest_seq, 종류, 참조, 배치 내용 해시)`의 입력 사슬을 만든다. 확정은 사슬을 0으로 되돌리고 `seal` 사건이 된다. 확정은 `applied_seq`와 `readable_through`가 같고 열린 적용이 없을 때만 되고, 그래프 해시가 앞 버전과 같으면 새 버전이 없다(빈 KG의 첫 확정도 없다). 선택으로 확정 전에 전체를 다시 계산해 견준다(`Verify`).
7. **리스.** `Acquire`는 `attempt`를 올린다. 다른 주인의 살아 있는 리스가 있거나, 머리가 적용한 의미 버전(정수)보다 낮으면 거절한다. 모든 쓰기는 기대하는 `applied_seq`, 세대, 리스(주인, `attempt`, 만료 시각)를 싣고 저장소가 확인한다. 의미 버전은 마지막 쪽에서 머리에 오른다.
8. **세대.** `Restore`가 세대를 올리고 사건을 남기며 이후의 행, 읽을 수 있는 번호, 버전을 회수한다. 머리는 적용기가 남은 행에서 다시 계산한다(`RecomputeHash`). 옛 세대의 읽기 점, 이미 잡은 스냅샷, 쓰기는 `graph_generation_changed`다.
9. **`volatile`.** 행과 해시에 들지 않고 최신 값 하나만 둔다. 바뀐 값만 `light_change`의 줄(새 값의 짧은 해시)이 되고, 같은 값은 변경이 아니다. 읽기 점은 `ReadLatest`(=`readable_through`, 값 있음), `ReadApplied`(적용기의 것, 값 있음), `ReadSeq`, `ReadVersion`(값 없음, `graph_volatile_omitted`와 `volatile_as_of: omitted`)이다. `change_seq`를 시간 여행 입력으로 받는다(덧붙임).
10. **포트의 네 역할.** `Catalog`(KG: 만들기, `main`을 포함한 찾기, 복구, 세대 목록), `Writer`(리스 쥐기, 늘리기, 놓기와 `ApplyChunk`, `AbandonApply`, `Seal`), `Reader`(`Head`, `Lease`, `Snapshot(ReadPoint)`, `Versions`), `Feed`(`AppliedChanges`, `LightChanges`, `OrderEvents`, `WaitReadable(min_seq, wait_timeout)`). 쓰기 셋은 머리 행을 잠그고 기대 머리, 세대, 리스를 확인한 뒤 모든 행과 머리를 한 트랜잭션으로 쓴다. 읽는 쪽은 쓰기가 끝나기 전의 쪽을 볼 수 없다. 행의 모양은 `kg-storage.md` 3절의 표를 따른다(부분합만 `type_id`로 바뀌었다).
11. **메모리 어댑터.** 한 뮤텍스 아래 구간이 있는 행의 사슬을 둔 완전하고 단순한 구현이다. 적합성 테스트를 모두 통과하고 `-race`에서 깨끗하며, 테스트와 개발 모드의 저장소이고 PostgreSQL 어댑터의 기준이다. 재시작하면 잃고 운영에서 쓰지 않는다(운영 모드의 조립이 이것을 거절하도록 하는 것은 조립을 만드는 변경이 한다).
12. **문서가 비었거나 어긋나서 고른 것.** 행의 `name`은 별개의 칸이고 `name` 속성과 겹쳐도 둘 다 해시에 든다. 업서트는 버전 속성을 통째로 바꾼다. 해시에 안 드는 칸(출처, 시각)만 달라진 업서트는 변경이 아니다. 속성 값은 문자열, `int64`, 불리언, 목록, 맵이고 수는 십진 문자열이다. 링크의 끝은 배치 뒤에 살아 있어야 하고, 링크가 남은 객체는 물릴 수 없다(링크를 같은 배치에서 물려야 한다). 카디널리티는 `:1` 쪽의 개수를 본다. 필수 속성, 매개 값 제약, 식별 키 값의 다듬기가 맞는지는 받는 쪽(`core.ingest`)의 일이라 보지 않는다. 물러난 객체의 `volatile` 값은 남지만 읽히지 않는다. 차례의 사건의 `ChangeSeq`는 그때의 `applied_seq`다. 부분합은 `type_id`로 센다.

## 이유

- 포트 하나와 기준 구현 하나, 같은 테스트로 어댑터를 가두면 PostgreSQL이 늦게 와도 위의 단위(질의, 길)를 지금 만들 수 있고, 어댑터가 뜻을 바꾸지 못한다.
- 결정을 코어에 두면(차이, 해시, 쪽 자르기) 어댑터가 둘이어도 ID, 번호, 해시, 버전이 같다(REQ-131의 바탕).
- 포기를 되감기로 정하면 큰 적용의 모든 실패에 닫힌 길이 생긴다.

## 대안과 버린 이유

- 적합성 테스트를 `core.graph` 안에 두기: 포트와 함께 바뀌어 낡지 않지만 테스트의 문장이 코어의 커버리지를 끌어내려 하한 90을 못 지킨다. 그래서 따로 두고, 포트가 바뀔 때 같은 변경에서 고친다.
- 저장소가 차이를 내기(SQL로): 어댑터마다 뜻이 갈라진다(ADR-0006의 "뜻의 구현은 하나").
- 포기를 앞으로 나아가는 변경으로 하기: 아무도 못 본 쪽에 새 변경과 번호를 쓰게 되어 번호가 낭비되고 버전이 늘어난다.
- 번호를 해시에 넣기: 번호를 바꾸는 이사에서 모든 해시가 바뀐다.

## 한계

- 적합성 테스트의 문은 이 구현이 아는 만큼이다. 어댑터가 새로 알게 되는 것(잠금 대기, 커넥션)은 어댑터의 테스트가 맡는다.
- 메모리 어댑터는 스캔과 링크 찾기가 정렬 목록 위라서 큰 KG에서 느리다. 규모는 PostgreSQL 어댑터에서 측정한다.
- 메모리 어댑터의 테스트는 적합성 테스트를 쓰지 않고 적용기로 직접 돌린다. `adapter.graphcheck`의 테스트가 메모리 어댑터를 돌리므로 반대로 읽으면 단위의 되돌이가 생기기 때문이다.
- 사전 정의 질의, 길, 계약, 명령줄, 개발용 적재, PostgreSQL은 이 변경에 없다.

## 요구사항 표의 변화

- **예정을 풀었다**: REQ-132, REQ-135.
- **나눴다**: REQ-131에서 REQ-246(변경 시퀀스부터의 결정성), REQ-232에서 REQ-247(가벼운 줄과 지난 읽기의 생략), REQ-237에서 REQ-248(세대), REQ-222에서 REQ-249(나눈 묶음과 기다림). 남은 REQ-131, REQ-222, REQ-232, REQ-237은 예정으로 있다.
- **테스트를 이었다**: REQ-233.
