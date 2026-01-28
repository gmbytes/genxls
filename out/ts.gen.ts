export interface Item {
  cid: number;
  count: number;
  data: string;
  dt: number[];
  dtArr: number[][];
}

export interface Quest {
  cid: number;
  count: number;
  data: string;
  dt: number[];
  dtArr: number[][];
}

export interface AllConfig {
  items: Item[];
  quests: Quest[];
}
