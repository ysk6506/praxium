---
title: ADR-0021 지어낸 세계 생성기가 core.worldgen 단위로 선다
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
decision_hash: a7c105ad5363d038
---
# ADR-0021 지어낸 세계 생성기가 core.worldgen 단위로 선다

## 맥락
제품 책임자가 시험 환경을 줄 수 없어서 규모와 정확함은 **지어낸 세계**로 확인한다(query-engine-shape.md 6절, REQ-134, REQ-230). 뒤 단계(수집, 식별, 변경 묶음, 영향 범위의 기준 구현)는 정답을 아는 입력이 필요하다. 표준 모델은 ADR-0020이 데이터 파일로 굳혔고 이제 그 위에서 세계를 지을 수 있다.

## 결정
- **단위.** `core.worldgen`(`backend/internal/worldgen`)은 `core.kernel`과 `core.model`만 읽는다. 운영체제와 네트워크를 읽지 않아서 파일은 `Store`로 받는다.
- **명령.** `praxium worldgen`은 `cmd.praxium`의 하위 명령이다. 둘째 실행 파일을 만들지 않는다(조립의 자리는 하나, ARCHITECTURE.md).
- **결정성.** 모든 무작위는 주입된 `kernel.Rng`에서 온다. 객체의 id와 속성 값은 주소에서 계산한다. 같은 인자면 정답과 소스 파일과 매니페스트가 바이트까지 같다.
- **표준 모델의 타입과 링크만.** 만든 세계는 등록부로 검사한다(`worldgen validate`). 키가 없는 타입(`CoolingLoop`)은 만들되 수집 불가로 표시한다.
- **흐름.** 객체와 링크를 만드는 즉시 관찰자에게 넘기고 쌓지 않는다. 메모리는 세계의 크기와 상관없다.
- **형식(v1).** 정답은 JSON Lines, 소스 보기는 CSV, JSON 쪽, 장치 설정 글, 고정 폭 표, JSON Lines, 모양 카드와 매니페스트는 JSON이다. 형식의 이름과 버전은 파일의 첫 줄이나 칸에 둔다. 형식은 `docs/design-docs/worldgen.md`가 정한다.
- **보호 경로.** `harness/config/architecture.yaml`에 단위를 등록하고, 커버리지 바닥을 올린다(`coverage-floor.json`의 `go:core.worldgen`). `AGENTS.md`의 명령 목록에 생성기를 돌리는 한 줄을 더한다(지침 파일이라 보호 경로이고 이 결정이 허락한다).

## 이유
- 주소에서 계산하면 저장하지 않고도 어떤 부분이든 다시 만들 수 있고, 소스 보기와 변경 계획이 대상을 산수로 찾는다. 1억 건이 15MB의 상주 메모리로 흐른다.
- 소스 보기에 흠을 인자로 넣고 매니페스트에 기대 결과를 적어서 뒤 단계의 시험이 "정답"을 갖는다.

## 대안과 버린 이유
- **세계를 메모리에 모았다 쓰기**: 수천만 건에서 메모리가 세계 크기에 비례한다.
- **별도 실행 파일**: 조립의 자리가 둘이 되고, 구조 검사가 이미 하나의 조립 자리를 요구한다.
- **`os`를 단위가 직접 쓰기**: 코어가 운영체제를 모르는 규칙(`GOARCH051`)을 깬다.

## 결과
- 생성기를 바꿔 세계가 달라지면 `golden_test.go`가 빨갛게 되어 의도한 변경인지 묻는다.
- 표준 모델의 빈 곳이 드러났다(`worldgen.md` 3절과 13절): 시설의 묶음(냉각 N+k, UPS)을 객체로 쓸 바인딩이 없고, 등록부는 `EcmpGroup`에 부모의 키를 물려줘서 ADR-0020 본문의 "키가 없는 타입" 목록과 다르다. 설계 담당이 정한다.
