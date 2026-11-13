---
title: 역량 질문의 예시 — 패턴으로 끝까지 적은 지어낸 물음
kind: reference
status: active
follows:
  - docs/design-docs/adr/0015-competency-questions.md
---
# 역량 질문의 예시

[역량 질문과 충족률](competency-questions.md)의 4.6절에서 떼어 낸 예시예요. 예시 ①(A·B 두 전원)은 본문에 있어요. 이름과 속성은 지어낸 것이고 패턴의 종류는 본문 4.1절을 따라요.

**예시 ② 이 VM이 도는 호스트가 죽으면 어디로 넘어가나.** `runs_on`과 `failover_to` 묶음이에요. 호스트는 관측으로 `fact`여야 하고, 넘겨받을 묶음의 `min_alive`와 구성원을 알아야 해요. 모르면 `impact`의 답은 `interrupted`가 아니라 `unknown`이에요.

```yaml
name: vm_failover_declared
target: {object_type: VirtualMachine}
need:
  kind: all_of
  items:
    - {need_id: host, kind: link_present, link_type: RUNS_ON, direction: out, peer_type: HypervisorHost, view: as_observed, min_grade: fact}
    - {need_id: failover_group, kind: group_evaluated, via: {meaning: runs_on, mediator: failover_to}, group_type: ComputeCluster, min_grade: assumed}
```

**예시 ③ 이 스위치가 죽으면 어느 서비스가 영향받는지 답할 수 있는가.** 이 스위치를 죽었다고 준 `impact`의 답에서 서비스 타입 객체 중 `unknown`이 없으면 채웠어요. 질의 엔진이 판정해요(6.4절).

```yaml
name: switch_impact_answerable
target: {object_type: Switch}
need: {need_id: impact_ok, kind: query_answers, query: impact, watch_types: [BusinessService, ApplicationService]}
```

**예시 ④ 이 서비스는 누가 맡는가.** 맡는 쪽은 사람이나 문서가 적은 것(`declared`)이라 `as_designed`로 읽고, `min_grade: fact`라서 기본값 규칙이 만든 `assumed`는 채우지 못해요.

```yaml
name: service_has_owner
target: {object_type: ApplicationService}
need: {need_id: owner, kind: link_present, link_type: OWNED_BY, direction: out, peer_type: Team, min_grade: fact, view: as_designed}
```
