This directory contains very hacky test automation for wireevaluator based on nested Ansible playbooks.

* Copy `inventory.example` to `inventory`
* Adjust `inventory` IP addresses as needed
* Make sure there's an OpenSSH server running on the serve host
* Make sure there's no firewalling whatsoever between the hosts
* Run `GENKEYS=1 ./gen_files.sh` to re-generate self-signed TLS certs
* Run the following command, adjusting the `wireevaluator_repeat` value to the number of times you want to repeat each test

```
ansible-playbook -i inventory all.yml -e `wireevaluator_repeat=3`
```

Generally, things are fine if the playbook doesn't show any panics from wireevaluator.

