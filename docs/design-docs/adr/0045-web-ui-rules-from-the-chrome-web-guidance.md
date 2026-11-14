---
title: ADR-0045 웹 UI 규칙을 Chrome 팀의 웹 가이드에서 추려 문서와 검사로 둔다
kind: decision
status: active
enforced_by:
  - WEB040
  - WEB041
  - WEB042
  - WEB043
  - WEB044
  - WEB045
  - harness/src/checks/web-rules.test.ts
  - apps/web/src/data/screen.test.tsx
  - backend/internal/httpapi/ui_headers_test.go
  - TRACE001
updates_docs:
  - AGENTS.md
  - docs/design-docs/web-ui-rules.md
  - docs/design-docs/temporary-console.md
  - docs/design-docs/harness-guide.md
  - docs/product-specs/requirements.md
touches_protected:
  - .github/workflows/ci.yml
  - AGENTS.md
  - THIRD_PARTY_NOTICES.md
  - docs/design-docs/adr/0035-web-ui-in-apps-web-embedded-in-the-program.md
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
  - harness/config/third-party-assets.yaml
  - harness/config/web.yaml
  - harness/config/coverage-floor.json
  - harness/src/checks/web-colors.ts
  - harness/src/checks/web-rules.test.ts
  - harness/src/checks/web-rules.ts
  - harness/src/checks/web.ts
amends:
  - adr: "0035"
    what: 6번 검사 표(WEB040~WEB045를 더함)와 2번의 응답 헤더(CSP 항목과 두 헤더를 더함)
decision_hash: 59686ec6a0336522
---
# ADR-0045 웹 UI 규칙을 Chrome 팀의 웹 가이드에서 추려 문서와 검사로 둔다

## 맥락

제품 책임자가 "웹 개발할 때 Google Chrome 팀의 modern-web-guidance(`GoogleChrome/modern-web-guidance-src`)를 참고하라"고 지시했다. 조사(커밋 5e5cd33)에서 이 저장소는 코딩 에이전트에게 가이드를 `npx`로 가져와 읽게 하는 것이 주된 용도이고, 가이드 252개 가운데 35개는 빈 틀이며, 본문은 CC BY 4.0이라는 것을 확인했다. 프락시움의 `apps/web`은 이미 대부분을 따랐지만, 화면 이동 때 제목과 포커스가 바뀌지 않고, 작은 글자와 입력란 테두리의 대비가 모자라고, 회전 표시가 움직임 줄이기를 무시하고, 보안 헤더가 한 줄의 `default-src`에 기대는 점이 어긋났다. 지원 브라우저를 정한 곳도 없었다.

## 결정

1. **참고하는 방식.** 가이드를 의존성으로 들이거나 `npx modern-web-guidance`를 에이전트가 실행하게 하지 않는다. 프락시움에 해당하는 것만 추려 [웹 UI 규칙](../web-ui-rules.md) 한 문서로 두고, AGENTS.md의 화면 코드 항목이 그 문서로 이끈다(줄 수는 그대로). 기계가 확인할 수 있는 규칙은 하네스 검사로 둔다. 상류가 바뀌었는지는 분기마다 사람이 해당 가이드만 읽어 확인한다.
2. **채택하지 않는 것.** `npx` 도구와 스킬(네트워크와 텔레메트리), 의존성을 늘리는 권고, 외부 서비스나 네트워크에 기대는 권고, 서버 렌더링을 전제한 권고, 서비스 워커와 PWA, Base UI를 네이티브 `<dialog>`로 바꾸는 일, Trusted Types 강제. 이유는 문서 10절에 있다.
3. **출처 표기.** 문서 끝에 저장소, 커밋, CC BY 4.0 링크, 바꾼 점을 적고 `third-party-assets.yaml`의 `references`에 올려 `THIRD_PARTY_NOTICES.md`에 나오게 한다. 가이드의 문장과 코드는 옮기지 않고 새로 썼다.
4. **지원 브라우저 기준.** 기본은 Baseline Widely available이다. Newly available은 기능 감지와 점진 향상일 때만, Limited와 오리진 트라이얼은 쓰지 않는다. 하한은 Tailwind CSS v4가 공식 문서로 요구하는 크롬·엣지 111, 파이어폭스 128, 사파리 16.4이다. 기업 환경의 크롬과 엣지는 업데이트가 느릴 수 있고 프락시움은 브라우저를 고를 수 없는 셀프 호스팅 제품이라서, 이미 빌드 결과가 의존하는 선에서 멈춘다. Vite의 `build.target`은 `es2022`에서 이 네 브라우저의 목록으로 바꾼다(언어 판이 아니라 브라우저 하한을 적는다). TypeScript `target`과 `lib`는 타입 검사용이라 그대로 둔다.
5. **코드 수정.** 화면 이동 때 `document.title`을 갱신하고 사용자가 옮긴 화면이면 포커스를 `h1`로 옮긴다(뒤로 가기와 쿼리 변경은 제외, 뒤로 가기는 `aria-live`로 알린다. `data/screen.ts`). 대비는 아래 표대로 고친다. 회전 표시는 `motion-safe:`로, 강제 색상 모드에서는 현재 메뉴, 선택한 탭, 배지, 회전 표시에 표시를 더한다. 컨트롤의 `h-8`은 `min-h-8`로, 넘치는 표의 스크롤 영역은 키보드로 닿게, 이동 경로는 `<ol>`로, 타입 검색은 `<search>`와 `type="search"`로 바꾼다. 응답 헤더는 CSP에 `script-src`, `connect-src`, `worker-src`, `object-src 'none'`을 명시하고 `Permissions-Policy`와 `Cross-Origin-Opener-Policy: same-origin`을 더한다. 외부 출처는 허용하지 않는다.
6. **새 검사.** 모두 `architecture` 검사 안의 `web.ts`에서 돌고, 설정은 `web.yaml`의 `rules`이다. 실패 메시지는 `problem`과 `fix`를 준다. 기존 규칙과 한도는 느슨하게 하지 않는다.

   | 규칙 | 내용 |
   |---|---|
   | `WEB040` | 토큰의 글자/배경 쌍은 4.5:1, 테두리와 포커스 링은 3:1 이상(라이트와 다크). 쌍은 설정에 선언하고 oklch를 sRGB로 바꾸는 계산은 `web-colors.ts`에 의존성 없이 구현했다 |
   | `WEB041` | 위험한 싱크: `innerHTML`·`outerHTML` 대입, `dangerouslySetInnerHTML`, `insertAdjacentHTML`, `setHTMLUnsafe`, `document.write`, `eval`, `new Function`, 문자열 `setTimeout`(테스트 파일 제외) |
   | `WEB042` | `apps/web/src`와 `index.html`의 `http(s)://` 절대 주소(허용 목록 없음. 테스트 픽스처 세 파일만 파일 단위로 제외) |
   | `WEB043` | `outline-none`과 `outline: none`은 같은 요소에 `focus-visible` 대체가 있어야 한다 |
   | `WEB044` | 움직이는 CSS가 있으면 `prefers-reduced-motion` 처리가 있어야 하고 `animate-`는 `motion-safe:` 뒤에서만 쓴다 |
   | `WEB045` | 빌드 결과의 JS와 CSS gzip 크기가 예산 이하(JS 170000바이트, CSS 10000바이트. 지금은 약 143000과 6000). 빌드 결과가 없으면 `pnpm check`는 건너뛰고, CI가 웹 빌드 뒤에 `web.ts --budget`로 돌린다(`ci.yml`에 단계 하나) |

   `WEB043`의 CSS 쪽은 규칙 블록의 선택자에 `:focus-visible`이 있거나 `:focus:not(:focus-visible)`일 때만 `outline: none`을 허용한다. 시스템 색 이름(`Highlight` 등)은 색 리터럴이 아니므로 `WEB020`이 막지 않는다.

### 대비 계산 (WCAG 2.x, 토큰 파일의 oklch를 직접 계산)

| 쌍 | 테마 | 전 | 후 |
|---|---|---|---|
| `subtle` 글자 / 표면(`surface`) | 라이트 | 4.28 | 5.50 |
| `subtle` 글자 / 바탕(`canvas`) | 라이트 | 3.98 | 5.12 |
| `subtle` 글자 / 들어간 면(`sunken`) | 라이트 | 3.81 | 4.90 |
| 컨트롤 테두리 / 표면 | 라이트 | 1.69 (`line-strong`) | 3.64 (`control-line`) |
| 컨트롤 테두리 / 표면 | 다크 | 1.92 (`line-strong`) | 3.49 (`control-line`) |
| 컨트롤 테두리 / 가장 불리한 면 | 라이트, 다크 | 1.50, 1.92 | 3.24, 3.49 |

다크 테마의 `subtle`은 이미 5.0 이상이라 바꾸지 않았다. 컨트롤 테두리는 새 토큰 `control-line`으로 분리했다(입력란, 선택 상자, 체크 박스, 보조 버튼, 메뉴 버튼). `line-strong`은 빈 상태 틀과 점선 밑줄 같은 장식에만 남겨 화면이 무거워지지 않게 했다.

### 따르지 않기로 한 것(보류)

- `aria-sort="none"` 제거: 잠긴 시험(`apps/web/src/ui/shell.test.tsx`)이 `"none"`을 단언한다. 시험을 바꾸려면 요구사항이 바뀐 것이어야 하고 그럴 만한 문제가 아니다(스크린 리더 동작에 영향이 작다). 정식 화면에서 다시 본다.
- 입력기 조합 중 `Enter` 무시(`isComposing`): 지금 `Enter`로 동작하는 직접 만든 핸들러가 없다. 만들 때 적용한다(문서 4절).
- `vh`를 `dvh`로: 이 도구는 데스크톱 중심이다. 새로 쓰는 코드부터 적용한다.
- Trusted Types 강제, 코드 분할, 로컬 스토리지 토큰: 문서 10절과 7절, ADR-0035의 결과에 이유가 있다.

## 이유

- 가이드를 실행 중에 부르게 하면 폐쇄망과 의존성 최소 원칙을 깬다. 추린 문서는 에이전트가 읽는 양이 적고 프락시움의 조건(버릴 화면 계층, 셀프 호스팅)을 반영한다.
- 규칙을 문서에만 두면 낡는다. 기계로 확실히 확인되는 것(대비 계산, 위험한 싱크, 주소, 예산)은 검사가 지키고, 에이전트는 실패 메시지로 규칙을 배운다.
- 지원 하한을 새로 정하지 않고 Tailwind의 요구에 맞춘 것은, 그 아래로 내려갈 수 없고 위로 올릴 이유도 없기 때문이다.

## 버린 대안

- 가이드 전체를 복사해 넣기: 해당 없는 것이 대부분이고 용량이 크며 상류가 바뀌면 낡는다.
- 접근성 자동 검사 도구(axe 등)를 개발 의존성으로 추가: WEB001의 절차가 필요하고 지금은 Playwright로 직접 확인하는 것으로 충분하다.
- `web-features` 데이터로 Baseline을 자동 판정: 의존성이다. 기준은 문서에 두고 사람이 갱신한다.
- `line-strong`을 3:1로 올리기: 장식 테두리까지 진해져 화면이 무거워진다.

## 결과

- 화면 코드를 쓰는 에이전트는 문서를 먼저 읽고, 어긋나면 `WEB040`~`WEB045`가 알린다.
- 번들 예산 검사는 CI의 웹 빌드 단계 뒤에서만 의미가 있다. 로컬의 `pnpm check`는 빌드가 없으면 건너뛴다.
- 화면 이동 때 포커스를 옮기는 동작은 `data/screen.ts`에 있고 `data/screen.test.tsx`가 확인한다. 실제 브라우저에서의 키보드 흐름, 움직임 줄이기와 강제 색상 모드 에뮬레이션, 200% 확대와 글자 간격 조정, 응답 헤더는 Playwright로 한 번 확인했다.
- 하한을 올릴 때(Tailwind를 올릴 때)는 문서 1절, `vite.config.ts`, `web-rules.test.ts`의 목록을 함께 바꾼다.
