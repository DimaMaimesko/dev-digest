import { x } from './x';

export async function loadRepo(id: string, opts?: { deep: boolean }): Promise<Repo> {
  return x(id);
}

function* ids(): Generator<number> {
  yield 1;
}

export class RepoStore extends Base implements Store {
  constructor(private db: Db) {
    super();
  }
  async get(id: string) {
    return this.db.find(id);
  }
  static create(): RepoStore {
    return new RepoStore(db);
  }
}

export interface Repo {
  id: string;
}

type Id = string | number;

export enum Color {
  Red,
}

export const toSlug = (name: string): string => name.toLowerCase();
const plain = 42;
let later = function named(a: number) {
  return a;
};
var gen = function* () {
  yield 1;
};

function helper(
  first: string,
  second: number,
) {
  return first;
}

export { helper, plain as renamed };
export default function main() {}
