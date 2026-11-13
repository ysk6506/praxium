---
title: ADR-0021 합성 세계 생성기를 core.worldgen 단위로 둔다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH051
  - GOARCH052
  - GOTEST002
  - GOCOV001
  - backend/internal/worldgen/golden_test.go
  - backend/internal/worldgen/world_prop_test.go
  - backend/internal/worldgen/views_test.go
  - backend/internal/worldgen/changes_test.go
  - backend/internal/worldgen/memory_test.go
  - backend/internal/worldgen/validate_test.go
  - backend/internal/worldgen/shape_test.go
  - backend/cmd/praxium/worldgen_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/product-design.md
  - docs/design-docs/worldgen.md
  - docs/exec-plans/active/plan.md
  - AGENTS.md
  - README.md
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - AGENTS.md
amends_tests: []
decision_hash: 96da88dfc83a40db
---
# ADR-0021 합성 세계 생성기를 core.worldgen 단위로 둔다

## 맥락
제품 책임자가 테스트 환경을 줄 수 없어서 규모와 정확성은 **합성 세계**로 확인한다(query-engine-shape.md 6절, REQ-134, REQ-230). 후속 단계(수집, 식별, 변경 배치, 영향 범위의 기준 구현)는 정답을 아는 입력이 필요하다. 표준 모델은 ADR-0020이 데이터 파일로 확정했고 이제 그 위에서 세계를 생성할 수 있다.

## 결정
- **단위.** `core.worldgen`(`backend/internal/worldgen`)은 `core.kernel`과 `core.model`만 읽는다. 운영체제와 네트워크에 접근하지 않아서 파일은 `Store`를 통해 받는다.
- **명령.** `praxium worldgen`은 `cmd.praxium`의 하위 명령이다. 두 번째 실행 파일을 만들지 않는다(조립 지점은 하나, ARCHITECTURE.md).
- **결정성.** 모든 난수는 주입된 `kernel.Rng`에서 온다. 객체의 id와 속성 값은 주소에서 계산한다. 같은 인자면 정답과 소스 파일과 매니페스트가 바이트까지 같다.
- **표준 모델의 타입과 링크만.** 생성한 세계는 등록부로 검증한다(`worldgen validate`). 키가 없는 타입(`CoolingLoop`)은 생성하되 수집 불가로 표시한다.
- **스트리밍.** 객체와 링크를 생성하는 즉시 옵저버에 전달하고 누적하지 않는다. 메모리 사용량은 세계의 크기와 무관하다.
- **형식(v1).** 정답은 JSON Lines, 소스 뷰는 CSV, JSON 페이지, 장치 설정 텍스트, 고정 폭 테이블, JSON Lines, 분포 프로파일과 매니페스트는 JSON이다. 형식의 이름과 버전은 파일의 첫 행이나 필드에 둔다. 형식은 `docs/design-docs/worldgen.md`가 정한다.
- **보호 경로.** `harness/config/architecture.yaml`에 단위를 등록하고, 커버리지 하한을 올린다(`coverage-floor.json`의 `go:core.worldgen`). `AGENTS.md`의 명령 목록에 생성기를 실행하는 한 줄을 추가한다(안내 파일이라 보호 경로이고 이 결정이 허용한다).

## 이유
- 주소에서 계산하면 저장하지 않고도 어떤 부분이든 다시 만들 수 있고, 소스 뷰와 변경 계획이 대상을 계산으로 찾는다. 1억 건을 15MB의 상주 메모리로 스트리밍한다.
- 소스 뷰에 결함을 인자로 주입하고 매니페스트에 기대 결과를 적어서 후속 단계의 테스트가 정답을 확보한다.

## 대안과 버린 이유
- **세계를 메모리에 모아 두고 쓰기**: 수천만 건에서 메모리가 세계 크기에 비례한다.
- **별도 실행 파일**: 조립 지점이 둘이 되고, 구조 검사가 이미 하나의 조립 지점을 요구한다.
- **`os`를 단위가 직접 사용하기**: 코어가 운영체제를 모르는 규칙(`GOARCH051`)을 깬다.

## 결과
- 생성기를 바꿔 세계가 달라지면 `golden_test.go`가 실패해 의도한 변경인지 확인하게 한다.
- 표준 모델의 미비한 부분이 드러났다(`worldgen.md` 3절과 13절): 시설의 그룹(냉각 N+k, UPS)을 객체로 표현할 바인딩이 없고, 등록부는 `EcmpGroup`이 부모의 키를 상속하게 해서 ADR-0020 본문의 "키가 없는 타입" 목록과 다르다. 설계 담당이 정한다.
