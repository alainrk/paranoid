Definition double (n : nat) := n + n.
Axiom double_even : forall n, Nat.even (double n) = true.
