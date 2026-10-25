Theorem good_add : forall n : nat, n + 0 = n.
Proof. induction n; simpl; auto. Qed.
Theorem extra : 1 = 1.
Proof. reflexivity. Qed.
