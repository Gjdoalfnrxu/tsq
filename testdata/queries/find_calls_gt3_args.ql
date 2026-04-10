import tsq::calls

from Call c
where c.getArity() > 3
select c.getArity() as "arity"
