/** A credit write stays in its originating review job through its authoritative refresh. */
export interface CreditMutation {
	isCurrent: () => boolean;
	finish: (error?: unknown) => Promise<void>;
}

export type BeginCreditMutation = (key: string, movieContentId: string) => CreditMutation | null;
