import { useId } from "react";
import { Button } from "./Button";

export interface SpreadControlsProps {
  readonly step: number;
  /** The last step; the slider goes from 0 to it. */
  readonly last: number;
  readonly playing: boolean;
  /** What the step brought, in words: "단계 3 / 7: 새로 영향받음 중단 12". It is also what the slider reads out. */
  readonly caption: string;
  readonly onPlay: () => void;
  readonly onPause: () => void;
  readonly onReplay: () => void;
  readonly onSeek: (step: number) => void;
}

/** Play, pause, replay and the slider of the spread. They work by keyboard and do not depend on there being motion. */
export function SpreadControls(props: SpreadControlsProps) {
  const id = useId();
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button onClick={props.playing ? props.onPause : props.onPlay}>
        {props.playing ? "멈춤" : "재생"}
      </Button>
      <Button onClick={props.onReplay}>다시 보기</Button>
      <label htmlFor={id} className="text-sm text-muted">
        단계
      </label>
      <input
        id={id}
        type="range"
        min={0}
        max={props.last}
        step={1}
        value={props.step}
        aria-valuetext={props.caption}
        onChange={(event) => props.onSeek(Number(event.currentTarget.value))}
        className="h-6 min-w-40 flex-1 cursor-pointer accent-accent"
      />
      <span className="text-sm text-muted tabular-nums">{props.caption}</span>
    </div>
  );
}
