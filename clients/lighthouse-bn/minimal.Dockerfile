ARG branch=latest

# TODO: either special upstream build, or clone + build minimal version here in dockerfile.
FROM sigp/lighthouse_minimal:$branch

ADD make_config.sh /make_config.sh
RUN chmod +x /make_config.sh

ADD lighthouse_bn.sh /lighthouse_bn.sh
RUN chmod +x /lighthouse_bn.sh

# TODO: make this work (broken in eth1 hive)
RUN echo "{\"repo\":\"\", \"branch\":\"\", \"commit\":\"\"}" > /version.json

ENTRYPOINT ["/lighthouse_bn.sh"]

