---
title: 역량 질문의 예시 — 패턴으로 끝까지 작성한 가상의 질문
kind: reference
status: active
follows:
  - docs/design-docs/adr/0015-competency-questions.md
---
# 역량 질문의 예시

[역량 질문과 충족률](competency-questions.md)의 4.6절에서 분리한 예시예요. 예시 ①(A·B 두 전원)은 본문에 있어요. 이름과 속성은 가상의 것이고 패턴의 종류는 본문 4.1절을 따라요.

**예시 ② 이 VM이 실행되는 호스트에 장애가 나면 어디로 장애 조치되는가.** `runs_on`과 `failover_to` 그룹이에요. 호스트는 관측으로 `fact`여야 하고, 인계받을 그룹의 `min_alive`와 구성원을 알아야 해요. 알 수 없으면 `unknown`이에요.

```yaml
name: vm_failover_declared
target: {object_type: VirtualMachine}
need:
  kind: all_of
  items:
    - {need_id: host, kind: link_present, link_type: RUNS_ON, direction: out, peer_type: HypervisorHost, view: as_observed, min_grade: fact}
    - {need_id: failover_group, kind: group_evaluated, via: {meaning: runs_on, mediator: failover_to}, group_type: ComputeCluster, min_grade: assumed}
```

**예시 ④ 이 서비스는 누가 담당하는가.** 담당 조직은 사람이나 문서가 기재한 것(`declared`)이라 `as_designed` 관점으로 읽고, `min_grade: fact`라서 기본값 규칙이 만든 `assumed`는 채울 수 없어요.

```yaml
name: service_has_owner
target: {object_type: ApplicationService}
need: {need_id: owner, kind: link_present, link_type: OWNED_BY, direction: out, peer_type: Team, min_grade: fact, view: as_designed}
```
