import { LEARNING_OBJECTIVES, MAIN_BRANCHES } from './constellation.data';

export const BOARD_SIZE = 4400;
export const ORIGIN = { x: BOARD_SIZE / 2, y: BOARD_SIZE / 2 };
export interface MapPoint {
  x: number;
  y: number;
}
export interface MapEdge {
  from: string;
  to: string;
  path: string;
  choice: boolean;
}

const ANGLES: Readonly<Record<string, number>> = {
  Personality: -135,
  Continuity: 180,
  Research: -45,
  Creation: 0,
  Workflows: 45,
  Connections: 135,
};
const WORKSHOPS: readonly [string, number, number][] = [
  ['Visual identity', -157, 650],
  ['Notebook lab', -20, 1080],
  ['Community remix', 68, 1130],
  ['Relay workshop', 158, 1100],
  ['Webhook workshop', 112, 1260],
  ['Self-host workshop', -112, 1050],
];

function polar(angle: number, radius: number): MapPoint {
  const radians = (angle * Math.PI) / 180;
  return { x: ORIGIN.x + Math.cos(radians) * radius, y: ORIGIN.y + Math.sin(radians) * radius };
}

function positions(): Readonly<Record<string, MapPoint>> {
  const points: Record<string, MapPoint> = { S1: ORIGIN };
  for (let i = 2; i <= 6; i++) points[`S${i}`] = polar(90, 330 + (i - 2) * 184);
  for (const branch of MAIN_BRANCHES)
    LEARNING_OBJECTIVES.filter(node => node.branch === branch).forEach(
      (node, i) => (points[node.id] = polar(ANGLES[branch], 340 + i * 230)),
    );
  ['B1', 'B2', 'B3'].forEach((id, i) => (points[id] = polar(-90, 530 + i * 195)));
  for (const [branch, angle, radius] of WORKSHOPS)
    LEARNING_OBJECTIVES.filter(node => node.branch === branch).forEach((node, i) => (points[node.id] = polar(angle, radius + i * 230)));
  return points;
}

export const NODE_POSITIONS = positions();

function edge(from: string, to: string, choice = false): MapEdge {
  const a = NODE_POSITIONS[from],
    b = NODE_POSITIONS[to];
  const dx = b.x - a.x,
    dy = b.y - a.y,
    length = Math.hypot(dx, dy);
  const pad = from === 'S1' ? 125 : 46;
  let path = `M ${a.x + (dx / length) * pad} ${a.y + (dy / length) * pad} L ${b.x - (dx / length) * 46} ${b.y - (dy / length) * 46}`;
  if (from === 'S6' && to === 'B1')
    path = `M ${a.x + 40} ${a.y} C ${ORIGIN.x + 1510} ${ORIGIN.y + 1250}, ${ORIGIN.x + 1510} ${ORIGIN.y - 1100}, ${b.x + 55} ${b.y}`;
  if (choice) path = `M ${a.x} ${a.y - 45} Q ${ORIGIN.x} ${ORIGIN.y - 1500} ${b.x} ${b.y + 95}`;
  return { from, to, path, choice };
}

export const MAP_EDGES: readonly MapEdge[] = [
  ...LEARNING_OBJECTIVES.flatMap(node => node.requires.map(parent => edge(parent, node.id))),
  ...MAIN_BRANCHES.map(branch => edge(LEARNING_OBJECTIVES.filter(node => node.branch === branch).at(-1)!.id, 'B1', true)),
];

export const MAP_LABELS = [
  ...MAIN_BRANCHES.map(branch => ({ title: branch, subtitle: 'A path from the origin', ...polar(ANGLES[branch], 1530) })),
  { title: 'Foundations', subtitle: 'Your first useful win', ...polar(90, 1330) },
  { title: 'The Builder', subtitle: 'Combine · create · repeat', ...polar(-90, 1080) },
  ...WORKSHOPS.map(([title, angle, radius]) => ({ title, subtitle: 'Optional mastery', ...polar(angle, radius + 420) })),
];
