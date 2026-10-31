Theorem fine : 1 = 1.
Proof. reflexivity. Qed.
Axiom magic : 1 = 2.
Theorem uses_axiom : 1 = 2.
Proof. exact magic. Qed.
Theorem hole : 2 = 2.
Proof. Admitted.
