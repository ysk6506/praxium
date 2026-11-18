import {
  type EntryMode,
  forgetToken,
  type Route,
  routeHash,
  ScreenReportsProvider,
  useEntryMode,
  useHash,
  useRoute,
  useScreen,
  useToken,
} from "./data";
import {
  KgsScreen,
  NotFoundScreen,
  ObjectScreen,
  ObjectsScreen,
  SettingsScreen,
  TypesScreen,
} from "./screens";
import { AppShell, Badge, Button, NavLink, NavSection } from "./ui";

/** What the strip above everything says about a way in that is not an ordinary one (ADR-0038, ADR-0042). */
const NOTICES: Readonly<Record<"demo" | "local", { notice: string; badge: string }>> = {
  demo: {
    notice: "데모 모드: 합성 데이터이며 실제 시스템과 연결되어 있지 않습니다. 운영용이 아닙니다.",
    badge: "데모",
  },
  local: {
    notice: "로컬 모드: 이 컴퓨터에서 혼자 쓰는 용도이며 운영용이 아닙니다.",
    badge: "로컬",
  },
};

const noticeOf = (mode: EntryMode | "none") =>
  mode === "demo" || mode === "local" ? NOTICES[mode] : undefined;

function currentKg(route: Route): string | undefined {
  return "kg" in route ? route.kg : undefined;
}

function Screen({ route, go }: { readonly route: Route; readonly go: (route: Route) => void }) {
  switch (route.page) {
    case "settings":
      return <SettingsScreen />;
    case "kgs":
      return <KgsScreen />;
    case "types":
      return <TypesScreen key={route.kg} kg={route.kg} />;
    case "objects":
      return <ObjectsScreen key={`${route.kg}/${route.type}`} kg={route.kg} type={route.type} />;
    case "object":
      return (
        <ObjectScreen
          key={`${route.kg}/${route.id}`}
          kg={route.kg}
          id={route.id}
          tab={route.tab}
          onTab={(tab) => go({ ...route, tab })}
        />
      );
    case "notFound":
      return <NotFoundScreen path={route.path} />;
  }
}

export function App() {
  const token = useToken();
  const mode = useEntryMode();
  const hash = useHash();
  const { route, go } = useRoute(token !== "");
  const screen = useScreen(route);
  const kg = currentKg(route);
  const notice = noticeOf(mode);
  const nav = (
    <>
      <NavSection title="탐색">
        <NavLink href={routeHash({ page: "kgs" })} current={route.page === "kgs"}>
          KG 목록
        </NavLink>
        {kg ? (
          <NavLink
            href={routeHash({ page: "types", kg })}
            current={route.page === "types" || route.page === "objects"}
          >
            객체 타입 ({kg})
          </NavLink>
        ) : null}
      </NavSection>
      <NavSection title="설정">
        <NavLink href={routeHash({ page: "settings" })} current={route.page === "settings"}>
          접속 설정
        </NavLink>
      </NavSection>
    </>
  );
  return (
    <AppShell
      brand={
        notice ? (
          <>
            프락시움 <Badge tone="warn">{notice.badge}</Badge>
          </>
        ) : (
          "프락시움"
        )
      }
      banner={notice?.notice}
      place={hash}
      announcement={screen.announcement}
      nav={nav}
      footer={
        token ? (
          <Button variant="ghost" onClick={() => forgetToken()}>
            토큰 삭제
          </Button>
        ) : (
          "토큰이 없습니다"
        )
      }
    >
      {/* A new token (another server run, a pasted address) is a new session: nothing of the old one stays on screen. */}
      <ScreenReportsProvider value={screen.report}>
        <Screen key={token} route={route} go={go} />
      </ScreenReportsProvider>
    </AppShell>
  );
}
