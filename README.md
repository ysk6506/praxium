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

**실제 IT 인프라를 믿을 수 있는 지식 그래프(KG)로 만들고, 그 위에서 AI 에이전트가 수행하는 엔드투엔드(E2E) 운영 업무를 정의·위임·측정하는 오픈소스 플랫폼.**

인프라는 지식 그래프로, 운영은 에이전트에게.

지금은 **KG를 짓고 운영하는 기반의 설계를 끝낸 단계**예요(ADR-0005~0018). 코드는 이제 시작이에요: `backend/`에 Go 모듈이 있고 공통 기반 코드(`core.kernel`), 타입 등록부와 표준 모델(`core.model`), 합성 세계 생성기(`core.worldgen`), KG 중심부와 스토리지 포트와 메모리 기준 구현과 PostgreSQL 구현(`core.graph`, `adapter.memstore`, ADR-0025, `adapter.pgstore`, ADR-0032), 사전 정의 질의의 첫 셋(`core.query`, ADR-0026), 그 질의를 API와 명령줄로 읽는 경로와 개발용 적재기(`adapter.devload`, ADR-0027), 진입점이 들어 있어요(ADR-0019, ADR-0020, ADR-0021). 백엔드는 Go로 만들고, 화면은 다른 리포지토리에서 REST API 계약으로만 통신해요(ADR-0006, ADR-0016). 지금 있는 TypeScript는 구조 검사(하네스, Go 코드도 읽어요)와 프런트엔드 리포지토리로 옮길 공통 기반 코드예요. 목표와 요구사항은 [요구사항](docs/product-specs/requirements.md)에, 설계 문서의 안내는 [제품 설계 개요](docs/design-docs/product-design.md)에 있어요. 테라리움 시절의 요구사항과 쟁점은 출발 자료로 [테라리움에서 이어받은 자료](docs/references/from-terrarium.md)에 남아 있어요.

## 브라우저로 보기

합성한 세계를 띄우고 임시 화면(`apps/web`)으로 KG를 둘러볼 수 있어요. 토큰은 지어낸 값이에요.

```
pnpm install
pnpm web:build /tmp/praxium
/tmp/praxium worldgen -scale 500 -seed 7 -out /tmp/world
printf 'dev-token-not-a-secret' > /tmp/dev.tok
/tmp/praxium api serve -mode development -dev-token-file /tmp/dev.tok -dev-truth /tmp/world/truth.jsonl
```

브라우저로 `http://127.0.0.1:8080`을 열고 접속 설정에 토큰을 붙여 넣어요. 외부 주소는 부르지 않아요. 설계는 [임시 콘솔](docs/design-docs/temporary-console.md)에 있어요.

## 이름

프락시움(Praxium)은 프락시스(praxis, 실천·행동)에서 온 이름이에요. [테라리움](https://github.com/ysk6506/terrarium)이 유리 안에서 에이전트를 테스트하는 곳이라면, 프락시움은 담쟁이가 벽을 타고 뻗어 나가듯 유리 밖의 현실에 닿아 일을 하는 곳이에요.

## 목표

1. **현실을 수집해 KG를 구축하고 운영해요.** 현실 시스템에서 구성 데이터를 읽어 객체와 링크로 변환하고, 무엇을 읽었고 무엇을 가정했는지 명시해요. 현실 시스템에는 읽기만 해요. 지금 구체화하는 것은 이 부분이에요.
2. **일을 엔드투엔드(E2E) 프로세스 단위로 정하고 에이전트에게 위임해요.** 누가 무엇을 어디까지 할 수 있는지, 누가 승인하는지, 무슨 일이 있었는지를 프락시움이 관리해요. 실제 툴은 에이전트가 MCP 게이트웨이를 통해서만 호출해요. 방향만 정해져 있고 구체화는 이후 단계예요.
3. **위임한 뒤에 측정해요.** 위임 전의 검증은 제품 밖에서 사용자가 수행해요.

## 테라리움과의 관계

프락시움은 별도 제품이에요(ADR-0005). 테라리움은 에이전트를 가상 세계에서 평가하는 실험장이고, 프락시움의 참고 자료일 뿐이에요. 두 제품은 서로를 호출하지 않고 연결되는 파일도 없어요. 위임 전의 검증은 사용자가 제품 밖(실험장)에서 독립적으로 수행해요. 프락시움은 현실 시스템에 읽기만 해요.

## 처음 10분

```
pnpm install
pnpm check
```

Go 1.27.4 이상이 있어야 해요. `pnpm check`가 Go의 `gofmt`, `go vet`, `go test`도 돌려요.

- 안내: [AGENTS.md](AGENTS.md)
- 구조: [ARCHITECTURE.md](ARCHITECTURE.md)
- 현재 진행 순서: [계획](docs/exec-plans/active/plan.md)
- 문서 전체: [색인](docs/index.md)

## 이 리포지토리의 작업 방식

코드는 코딩 에이전트가 작성하고, 리포지토리는 에이전트가 방향을 잃지 않도록 구성돼 있어요. 규칙은 말이 아니라 검사로 강제해요. 의존 방향, 문서의 최신 여부, 테스트를 수정해서 통과시키지 못하게 하는 잠금, 부채의 기한이 그 대상이에요. 이 구조는 테라리움에서 가져왔어요([ADR-0002](docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md)).

리포지토리에는 공개된 사실만 둬요. 내부에서 들은 사실, 실제 데이터, 시크릿은 넣지 않아요([ADR-0003](docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md)).

## 라이선스

Apache License 2.0 ([LICENSE](LICENSE)). 함께 나가는 제3자 구성요소는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있어요.
