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

**현실의 IT 인프라를 데이터로 받아 운영 세계를 짓고, 그 위에서 에이전트가 맡는 E2E 프로세스를 정하고 맡기고 재는 사내 운영 관리 플랫폼.**

지금은 **구조만 세운 단계**예요. 제품 기능은 아직 없어요. 목표와 요구사항은 [요구사항](docs/product-specs/requirements.md)에, 테라리움 때의 요구사항·설계 결정·쟁점은 [넘겨받은 것](docs/references/from-terrarium.md)에 있어요.

## 이름

프락시움(Praxium)은 프락시스(praxis, 실천·행동)에서 온 이름이에요. [테라리움](https://github.com/ysk6506/terrarium)이 유리 안에서 에이전트를 시험하는 곳이라면, 프락시움은 담쟁이가 벽을 타고 뻗어 나가듯 유리 밖의 현실에 닿아 일을 하는 곳이에요.

## 무엇을 하려는 곳인가요

1. **현실을 받아 운영 세계를 지어요.** 현실 시스템에서 구성 데이터를 읽어 객체와 링크로 맞추고, 무엇을 읽었고 무엇을 가정했는지 밝혀요. 현실 시스템에는 읽기만 해요.
2. **일을 E2E 프로세스 단위로 정하고 에이전트에게 맡겨요.** 누가 무엇을 어디까지 해도 되는지, 누가 승인하는지, 무슨 일이 있었는지를 프락시움이 맡아요. 실제 툴은 에이전트가 MCP 게이트웨이를 통해서만 불러요.
3. **맡기기 전에 검증하고, 맡긴 뒤에도 재요.** 검증은 테라리움이 해요. 프락시움은 세계 스냅샷을 내주고 판정 기록을 받아요.

## 테라리움과의 관계

| | 테라리움 | 프락시움 |
|---|---|---|
| 무엇 | 실험장: 에이전트를 가상 세계에서 평가 | 운영 관리: 현실의 세계와 에이전트가 맡는 일 |
| 현실 시스템 | 닿지 않아요 | 읽기만 해요 |
| 멈추면 | 실험이 멈춰요 | 맡긴 일의 관리가 멈춰요 |

둘은 파일 둘로만 이어져요. 프락시움이 **세계 스냅샷**을 내주고, 테라리움이 **판정 기록**을 내줘요. 서로를 부르지 않아요.

## 처음 10분

```
pnpm install
pnpm check
```

- 길 찾기: [AGENTS.md](AGENTS.md)
- 구조: [ARCHITECTURE.md](ARCHITECTURE.md)
- 지금의 순서: [계획](docs/exec-plans/active/plan.md)
- 문서 전체: [색인](docs/index.md)

## 이 저장소가 일하는 방식

코드를 쓰는 것은 코딩 에이전트이고, 저장소는 에이전트가 길을 잃지 않게 만들어져 있어요. 규칙은 말이 아니라 검사가 지켜요: 의존 방향, 문서가 낡았는지, 시험을 고쳐서 통과시키지 못하게 하는 잠금, 부채의 기한. 구조는 테라리움에서 가져왔어요([ADR-0002](docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md)).

저장소에는 공개된 사실만 둬요. 내부에서 들은 사실, 실제 데이터, 비밀 값은 넣지 않아요([ADR-0003](docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md)).

## 라이선스

Apache License 2.0 ([LICENSE](LICENSE)). 함께 나가는 제3자 구성요소는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있어요.
