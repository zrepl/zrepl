# This Spec file packages pre-built artifacts in the artifacts directory
# into an RPM. This means that rpmbuild won't actually build any new artifacts
#
# Read the Makefile first, then come back here.

# Global meta data
Version:          SUBSTITUTED_BY_MAKEFILE
%global common_description %{expand:
zrepl is a one-stop, integrated solution for ZFS replication.}

# use gzip to be compatible with centos7
%define _source_payload w9.gzdio
%define _binary_payload w9.gzdio

# don't strip pre-built binaries
%define __strip /usr/bin/true

Name:             zrepl
Release:          SUBSTITUTED_BY_MAKEFILE
Summary:          One-stop, integrated solution for ZFS replication
License:          MIT
URL:              https://zrepl.github.io/
# Source: we use rpmbuild --build-in-tree => no source
BuildRequires:    systemd
Requires(post):   systemd
Requires(preun):  systemd
Requires(postun): systemd

%description
%{common_description}

%prep
# we use rpmbuild --build-in-tree => no prep or setup

%build
# we don't actually need to build anything here, that has already been done by the Makefile

# Correct the path in the systemd unit file
sed s:/usr/local/bin/:%{_bindir}/:g dist/systemd/zrepl.service > artifacts/rpmbuild/zrepl.service

# Generate the default configuration file
sed 's#USR_SHARE_ZREPL#%{_datadir}/doc/zrepl#' packaging/systemd-default-zrepl.yml > artifacts/rpmbuild/zrepl.yml

%install
install -Dm 0755 artifacts/%{_zrepl_binary_filename}    %{buildroot}%{_bindir}/zrepl
install -Dm 0644 artifacts/rpmbuild/zrepl.service       %{buildroot}%{_unitdir}/zrepl.service
install -Dm 0644 artifacts/_zrepl.zsh_completion        %{buildroot}%{_datadir}/zsh/site-functions/_zrepl
install -Dm 0644 artifacts/bash_completion              %{buildroot}%{_datadir}/bash-completion/completions/zrepl
install -Dm 0644 artifacts/rpmbuild/zrepl.yml           %{buildroot}%{_sysconfdir}/zrepl/zrepl.yml
install -d                                              %{buildroot}%{_datadir}/doc/zrepl
cp -a   artifacts/docs/html                             %{buildroot}%{_datadir}/doc/zrepl/html
cp -a   internal/config/samples                         %{buildroot}%{_datadir}/doc/zrepl/examples

%post
%systemd_post zrepl.service


%preun
%systemd_preun zrepl.service


%postun
%systemd_postun_with_restart zrepl.service


%files
%defattr(-,root,root)
%license LICENSE
%{_bindir}/zrepl
%config %{_unitdir}/zrepl.service
%dir %{_sysconfdir}/zrepl
%config %{_sysconfdir}/zrepl/zrepl.yml
%{_datadir}/zsh/site-functions/_zrepl
%{_datadir}/bash-completion/completions/zrepl
%{_datadir}/doc/zrepl

%changelog
# TODO: auto-fill changelog from git? -> need same solution for debian
