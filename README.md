---
title: Praxium — 현실의 인프라 위에서 에이전트가 일하게 하는 운영 관리 플랫폼
kind: guide
status: active
covers:
  - package.json
follows:
  - docs/exec-plans/active/plan.md
---
# Praxium

**실제 IT 인프라의 데이터를 수집해 지식 그래프(KG)로 구축하고, 그 위에서 에이전트가 수행하는 엔드투엔드(E2E) 업무 프로세스를 정의·위임·측정하는 운영 관리 플랫폼.**

지금은 **KG를 짓고 운영하는 기반의 설계를 끝낸 단계**예요(ADR-0005~0018). 코드는 이제 시작이에요: `backend/`에 Go 모듈이 있고 공통 기반 코드(`core.kernel`), 타입 등록부와 표준 모델(`core.model`), 합성 세계 생성기(`core.worldgen`), KG의 중심과 저장소 포트와 메모리 기준 구현(`core.graph`, `adapter.memstore`, ADR-0025), 사전 정의 질의의 첫 셋(`core.query`, ADR-0026), 그 질의를 API와 명령줄로 읽는 경로와 개발용 적재기(`adapter.devload`, ADR-0027), 시작점이 들어 있어요(ADR-0019, ADR-0020, ADR-0021). 백엔드는 Go로 만들고, 화면은 다른 저장소에서 REST API의 계약으로만 대화해요(ADR-0006, ADR-0016). 지금 있는 TypeScript는 구조 검사(하네스, Go 코드도 읽어요)와 프런트엔드 저장소로 옮길 공통 기반 코드예요. 목표와 요구사항은 [요구사항](docs/product-specs/requirements.md)에, 설계 문서의 지도는 [제품 설계 한 장](docs/design-docs/product-design.md)에 있어요. 테라리움 때의 요구사항과 쟁점은 출발 자료로 [넘겨받은 것](docs/references/from-terrarium.md)에 남아 있어요.

## 이름

프락시움(Praxium)은 프락시스(praxis, 실천·행동)에서 온 이름이에요. [테라리움](https://github.com/ysk6506/terrarium)이 유리 안에서 에이전트를 테스트하는 곳이라면, 프락시움은 담쟁이가 벽을 타고 뻗어 나가듯 유리 밖의 현실에 닿아 일을 하는 곳이에요.

## 무엇을 하려는 곳인가요

1. **현실을 받아 KG(KG)를 짓고 운영해요.** 현실 시스템에서 구성 데이터를 읽어 객체와 링크로 맞추고, 무엇을 읽었고 무엇을 가정했는지 밝혀요. 현실 시스템에는 읽기만 해요. 지금 구체화하는 것은 이 부분이에요.
2. **일을 E2E 프로세스 단위로 정하고 에이전트에게 위임해요.** 누가 무엇을 어디까지 해도 되는지, 누가 승인하는지, 무슨 일이 있었는지를 프락시움이 맡아요. 실제 툴은 에이전트가 MCP 게이트웨이를 통해서만 불러요. 방향만 있고 구체화는 뒤예요.
3. **위임한 뒤에 재요.** 맡기기 전의 검증은 제품 밖에서 사용자가 해요.

## 테라리움과의 관계

프락시움은 별도 제품이에요(ADR-0005). 테라리움은 에이전트를 가상 세계에서 평가하는 실험장이고, 프락시움의 참고 자료일 뿐이에요. 두 제품은 서로를 부르지 않고 이어지는 파일도 없어요. 맡기기 전의 검증은 사용자가 제품 밖(실험장)에서 독립적으로 해요. 프락시움은 현실 시스템에 읽기만 해요.

## 처음 10분

```
pnpm install
pnpm check
```

Go 1.24 이상이 있어야 해요. `pnpm check`가 Go의 `gofmt`, `go vet`, `go test`도 돌려요.

- 길 찾기: [AGENTS.md](AGENTS.md)
- 구조: [ARCHITECTURE.md](ARCHITECTURE.md)
- 지금의 순서: [계획](docs/exec-plans/active/plan.md)
- 문서 전체: [색인](docs/index.md)

## 이 저장소가 일하는 방식

코드를 쓰는 것은 코딩 에이전트이고, 저장소는 에이전트가 길을 잃지 않게 만들어져 있어요. 규칙은 말이 아니라 검사가 지켜요: 의존 방향, 문서가 낡았는지, 테스트를 고쳐서 통과시키지 못하게 하는 잠금, 부채의 기한. 구조는 테라리움에서 가져왔어요([ADR-0002](docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md)).

저장소에는 공개된 사실만 둬요. 내부에서 들은 사실, 실제 데이터, 비밀 값은 넣지 않아요([ADR-0003](docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md)).

## 라이선스

Apache License 2.0 ([LICENSE](LICENSE)). 함께 나가는 제3자 구성요소는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있어요.
