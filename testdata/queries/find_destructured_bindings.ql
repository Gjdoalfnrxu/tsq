import tsq::expressions

from DestructureField df
select df.getSourceField() as "source", df.getBindName() as "binding"
