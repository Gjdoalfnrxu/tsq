import tsq::imports

from ImportBinding ib
select ib.getModuleSpec() as "module", ib.getImportedName() as "imported"
